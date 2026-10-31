// Compile alongside Client.cs with /main:DevBox.RdpSmoke.MstscProgram.
// This harness launches the inbox mstsc.exe and observes its actual desktop pixels.
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Drawing2D;
using System.Drawing.Imaging;
using System.Globalization;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using System.Windows.Automation;
using System.Windows.Forms;

namespace DevBox.RdpSmoke
{
    internal static class MstscProfile
    {
        internal static string Parse(string contents)
        {
            if (contents == null || contents.Length > 32768) throw Invalid();
            var fields = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            foreach (string line in contents.Split(new[] { '\r', '\n' }, StringSplitOptions.RemoveEmptyEntries))
            {
                string[] parts = line.Split(new[] { ':' }, 2);
                if (parts.Length != 2 || fields.ContainsKey(parts[0]) || line.IndexOf('\0') >= 0) throw Invalid();
                fields.Add(parts[0], parts[1]);
            }
            // This is deliberately a separate, exact schema from the downloaded
            // gateway profile. Non-NLA fixture sessions must never escape loopback.
            string[] expected = {
                "username:s:rdp-smoke", "screen mode id:i:1", "use multimon:i:0",
                "desktopwidth:i:1024", "desktopheight:i:768", "session bpp:i:32",
                "authentication level:i:1", "enablecredsspsupport:i:0", "negotiate security layer:i:1",
                "prompt for credentials:i:0", "prompt for credentials on client:i:0",
                "autoreconnection enabled:i:0", "redirectclipboard:i:0", "redirectprinters:i:0",
                "redirectcomports:i:0", "redirectsmartcards:i:0", "redirectwebauthn:i:0", "drivestoredirect:s:",
                "audiomode:i:2", "audiocapturemode:i:0", "smart sizing:i:1",
                "dynamic resolution:i:0", "disableconnectionsharing:i:1"
            };
            if (fields.Count != expected.Length + 1) throw Invalid();
            foreach (string field in expected)
            {
                string[] parts = field.Split(new[] { ':' }, 2);
                string value;
                if (!fields.TryGetValue(parts[0], out value) || value != parts[1]) throw Invalid();
            }
            string address;
            if (!fields.TryGetValue("full address", out address) || !address.StartsWith("s:127.0.0.1:", StringComparison.Ordinal))
                throw Invalid();
            string portText = address.Substring("s:127.0.0.1:".Length);
            int port;
            if (!Int32.TryParse(portText, NumberStyles.None, CultureInfo.InvariantCulture, out port) || port < 1 || port > 65535 ||
                port.ToString(CultureInfo.InvariantCulture) != portText) throw Invalid();
            return address.Substring(2);
        }

        private static SmokeFailure Invalid() { return new SmokeFailure("configuration", "invalid_mstsc_loopback_profile"); }
    }

    internal sealed class MstscObservation
    {
        private int candidate = -1, samples, expectedLayout = -1;
        private double inputSubmittedAt;
        private bool correctLayout;
        internal int InitialLayout = -1, Phase, LayoutBefore = -1;
        internal bool InputSubmitted, InputConfirmed;
        internal double FirstFrameAt, LastGoodAt;
        internal readonly HeartbeatMonitor Heartbeat = new HeartbeatMonitor();
        internal HeartbeatMonitor PhaseHeartbeat = new HeartbeatMonitor();

        internal string PhaseName { get { return Phase == 0 ? "initial" : Phase == 1 ? "shrink" : "restore"; } }
        internal bool ReadyForInput { get { return LayoutBefore >= 0 && correctLayout && !InputSubmitted; } }

        internal void SubmittedInput(double now)
        {
            if (!ReadyForInput) throw new SmokeFailure("input", "unexpected_mstsc_input");
            InputSubmitted = true;
            inputSubmittedAt = now;
        }

        internal bool PhaseVerified(double now)
        {
            return InputConfirmed && correctLayout && PhaseHeartbeat.Verified(now);
        }

        internal void BeginResize(double now)
        {
            if (Phase >= 2 || !PhaseVerified(now)) throw new SmokeFailure("resize", "unexpected_mstsc_resize");
            Phase++;
            LayoutBefore = candidate = -1;
            samples = 0;
            InputSubmitted = InputConfirmed = false;
            PhaseHeartbeat = new HeartbeatMonitor();
        }

        // Call with an invalid layout until the measured window has the exact
        // requested dimensions. Three stable frames precede each new F8 probe.
        internal bool Observe(int layout, int marker, double now)
        {
            if (LayoutBefore < 0)
            {
                if (layout < 0 || (InitialLayout >= 0 && layout != expectedLayout)) { candidate = -1; samples = 0; }
                else if (candidate != layout) { candidate = layout; samples = 1; }
                else samples++;
                if (samples >= 3)
                {
                    LayoutBefore = expectedLayout = layout;
                    if (InitialLayout < 0)
                    {
                        InitialLayout = layout;
                        FirstFrameAt = LastGoodAt = now;
                    }
                }
                if (InitialLayout < 0)
                {
                    if (now > 45) throw new SmokeFailure("render", "fixture_not_visible");
                    return false;
                }
            }
            Heartbeat.Observe(marker, now);
            if (LayoutBefore >= 0) PhaseHeartbeat.Observe(marker, now);
            if (Heartbeat.Stalled(now, FirstFrameAt)) throw new SmokeFailure("render", "heartbeat_stalled");
            if (InputSubmitted && layout == 1 - LayoutBefore)
            {
                InputConfirmed = true;
                expectedLayout = layout;
            }
            if (InputSubmitted && !InputConfirmed && now - inputSubmittedAt > 10)
                throw new SmokeFailure("input", "fixture_did_not_change");
            correctLayout = layout == expectedLayout;
            if (correctLayout) LastGoodAt = now;
            if (now - LastGoodAt > 3) throw new SmokeFailure("render", "fixture_lost");
            return Phase == 2 && PhaseVerified(now) && layout == expectedLayout &&
                now - FirstFrameAt >= 30 && Heartbeat.Verified(now);
        }
    }

    internal static class MstscResize
    {
        internal static Size Smaller(Size original)
        {
            var result = new Size(Math.Min(800, original.Width - 80), Math.Min(600, original.Height - 60));
            if (result.Width < 700 || result.Height < 540)
                throw new SmokeFailure("infrastructure", "desktop_too_small_for_resize");
            return result;
        }
    }

    internal static class MstscPixels
    {
        // Smart sizing can stretch a 1024x768 session to fit the available
        // client area, or letterbox it. Locate the fixture's four color edges,
        // verify that both quadrant seams remain centered, then normalize only
        // that rectangle. The marker still represents displayed screen pixels.
        internal static Rectangle Bounds(Bitmap image)
        {
            if (image.Width < 640 || image.Height < 480) return Rectangle.Empty;
            int layout = Fixture.Match(Fixture.Samples(image));
            if (layout < 0) return Rectangle.Empty;
            Color topLeft = Fixture.Colors[Fixture.Layouts[layout][0]];
            Color topRight = Fixture.Colors[Fixture.Layouts[layout][1]];
            Color bottomLeft = Fixture.Colors[Fixture.Layouts[layout][2]];
            int left = 0, right = image.Width - 1, top = 0, bottom = image.Height - 1;
            while (left < image.Width / 2 && !Fixture.Near(image.GetPixel(left, image.Height / 4), topLeft, 40)) left++;
            while (right > image.Width / 2 && !Fixture.Near(image.GetPixel(right, image.Height / 4), topRight, 40)) right--;
            while (top < image.Height / 2 && !Fixture.Near(image.GetPixel(image.Width / 4, top), topLeft, 40)) top++;
            while (bottom > image.Height / 2 && !Fixture.Near(image.GetPixel(image.Width / 4, bottom), bottomLeft, 40)) bottom--;
            var result = Rectangle.FromLTRB(left, top, right + 1, bottom + 1);
            if (result.Width < 640 || result.Height < 480) return Rectangle.Empty;
            int centerX = result.Left + result.Width / 2, centerY = result.Top + result.Height / 2;
            // Complete nonuniform scaling keeps each quadrant boundary centered.
            // Cropping the desktop instead shifts a seam relative to these edges.
            // Stay clear of the seam itself to tolerate scaling and codec filters.
            for (int i = 0; i < 4; i++)
            {
                bool rightHalf = i % 2 != 0, bottomHalf = i >= 2;
                int sampleX = result.Left + result.Width * (rightHalf ? 3 : 1) / 4;
                int sampleY = result.Top + result.Height * (bottomHalf ? 3 : 1) / 4;
                Color expected = Fixture.Colors[Fixture.Layouts[layout][i]];
                if (!Fixture.Near(image.GetPixel(centerX + (rightHalf ? 4 : -4), sampleY), expected, 40) ||
                    !Fixture.Near(image.GetPixel(sampleX, centerY + (bottomHalf ? 4 : -4)), expected, 40))
                    return Rectangle.Empty;
            }
            return result;
        }

        internal static Bitmap Normalize(Bitmap image, Rectangle bounds)
        {
            var result = new Bitmap(1024, 768, PixelFormat.Format24bppRgb);
            using (Graphics graphics = Graphics.FromImage(result))
            {
                graphics.InterpolationMode = InterpolationMode.NearestNeighbor;
                graphics.PixelOffsetMode = PixelOffsetMode.Half;
                graphics.DrawImage(image, new Rectangle(0, 0, 1024, 768), bounds, GraphicsUnit.Pixel);
            }
            return result;
        }
    }

    internal sealed class MstscProcess : IDisposable
    {
        private IntPtr job, process;
        internal readonly uint Id;

        internal MstscProcess(string profilePath)
        {
            IntPtr attributes = IntPtr.Zero, jobValue = IntPtr.Zero;
            bool attributesInitialized = false;
            try
            {
                job = MstscNative.CreateJobObject(IntPtr.Zero, null);
                if (job == IntPtr.Zero) throw new SmokeFailure("infrastructure", "mstsc_job_creation_failed");
                var limits = new MstscNative.JobLimits();
                limits.Basic.LimitFlags = 0x2000; // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
                if (!MstscNative.SetInformationJobObject(job, 9, ref limits, (uint)Marshal.SizeOf(limits)))
                    throw new SmokeFailure("infrastructure", "mstsc_job_configuration_failed");
                // JOB_LIST associates the process at creation, closing the orphan
                // race between CreateProcess(CREATE_SUSPENDED) and job assignment.
                // Supported by Windows 10 / Server 2016 and later.
                IntPtr size = IntPtr.Zero;
                MstscNative.InitializeProcThreadAttributeList(IntPtr.Zero, 1, 0, ref size);
                if (size == IntPtr.Zero) throw new SmokeFailure("infrastructure", "mstsc_job_attributes_failed");
                attributes = Marshal.AllocHGlobal(size);
                if (!MstscNative.InitializeProcThreadAttributeList(attributes, 1, 0, ref size))
                    throw new SmokeFailure("infrastructure", "mstsc_job_attributes_failed");
                attributesInitialized = true;
                jobValue = Marshal.AllocHGlobal(IntPtr.Size);
                Marshal.WriteIntPtr(jobValue, job);
                if (!MstscNative.UpdateProcThreadAttribute(attributes, 0, new IntPtr(0x2000d), jobValue,
                    new IntPtr(IntPtr.Size), IntPtr.Zero, IntPtr.Zero))
                    throw new SmokeFailure("infrastructure", "mstsc_job_attributes_failed");
                string executable = Path.Combine(Environment.SystemDirectory, "mstsc.exe");
                var startup = new MstscNative.StartupInfoEx();
                startup.Startup.Size = Marshal.SizeOf(startup);
                startup.Attributes = attributes;
                MstscNative.ProcessInfo created;
                var command = new StringBuilder(Quote(executable) + " " + Quote(profilePath) + " /public /w:1024 /h:768");
                if (!MstscNative.CreateProcess(executable, command, IntPtr.Zero, IntPtr.Zero, false, 0x80000,
                    IntPtr.Zero, null, ref startup, out created))
                    throw new SmokeFailure("client", "mstsc_process_launch_failed");
                process = created.Process;
                Id = created.ProcessId;
                MstscNative.CloseHandle(created.Thread);
            }
            catch { Dispose(); throw; }
            finally
            {
                if (attributesInitialized) MstscNative.DeleteProcThreadAttributeList(attributes);
                if (attributes != IntPtr.Zero) Marshal.FreeHGlobal(attributes);
                if (jobValue != IntPtr.Zero) Marshal.FreeHGlobal(jobValue);
            }
        }

        internal static string Quote(string path)
        {
            if (String.IsNullOrEmpty(path) || path.IndexOfAny(new[] { '"', '\r', '\n', '\0' }) >= 0 || path.EndsWith("\\", StringComparison.Ordinal))
                throw new SmokeFailure("configuration", "invalid_mstsc_path");
            return "\"" + path + "\"";
        }

        internal bool Running { get { return process != IntPtr.Zero && MstscNative.WaitForSingleObject(process, 0) == 258; } }

        internal void Stop(Report report)
        {
            if (job != IntPtr.Zero) { MstscNative.CloseHandle(job); job = IntPtr.Zero; }
            if (process == IntPtr.Zero) return;
            if (MstscNative.WaitForSingleObject(process, 5000) != 0)
                throw new SmokeFailure("cleanup", "mstsc_process_cleanup_timeout");
            uint exitCode;
            if (!MstscNative.GetExitCodeProcess(process, out exitCode))
                throw new SmokeFailure("cleanup", "mstsc_process_exit_unavailable");
            if (report != null) report.Add("client", "passed", "mstsc_process_exited",
                new { client = "mstsc.exe", processId = Id, exitCode = exitCode });
            MstscNative.CloseHandle(process);
            process = IntPtr.Zero;
        }

        public void Dispose()
        {
            if (job != IntPtr.Zero) { MstscNative.CloseHandle(job); job = IntPtr.Zero; }
            if (process != IntPtr.Zero) { MstscNative.WaitForSingleObject(process, 5000); MstscNative.CloseHandle(process); process = IntPtr.Zero; }
        }
    }

    // Kept free of UI Automation types so the consent policy can be tested offline.
    internal sealed class MstscDialogControl
    {
        internal string Kind, Name, Label, Value;
        internal bool Owned, Enabled, Visible, Password, ReadOnly, CanInvoke;
        internal int Toggle = -1;
    }

    internal sealed class MstscDialogDecision
    {
        internal string Kind, Reason;
        internal int Button = -1, Check = -1;
        internal bool Allowed { get { return Reason == null; } }
    }

    internal static class MstscDialogPolicy
    {
        internal const string Education = "Opening Remote Desktop Connection";
        internal const string EducationalCheck = "I understand and allow RDP files to open on this device for my account";
        internal const string Warning = "Caution: Unknown remote connection";
        internal const string LegacyWarning = "The publisher of this remote connection can't be identified. Do you want to connect anyway?";

        // Accelerators belong to labels, never to the remote address itself.
        internal static string Label(string text)
        {
            return String.Join(" ", (text ?? "").Replace("&", "").Split((char[])null, StringSplitOptions.RemoveEmptyEntries));
        }

        internal static bool Field(string label, string expected)
        {
            return Label(label).TrimEnd(':') == expected;
        }

        private static bool HasText(IList<MstscDialogControl> controls, string text)
        {
            foreach (MstscDialogControl control in controls)
                if (control.Owned && control.Visible && !control.Password && control.Kind == "Text" && Label(control.Name) == text)
                    return true;
            return false;
        }

        private static string StaticField(IList<MstscDialogControl> controls, int index, string label)
        {
            if (index + 1 >= controls.Count) return null;
            MstscDialogControl name = controls[index], value = controls[index + 1];
            if (name.Kind != "Text" || !name.Owned || !name.Visible || name.Password || !Field(name.Name, label) ||
                value.Kind != "Text" || !value.Owned || !value.Visible || value.Password) return null;
            return (value.Name ?? "").Trim();
        }

        private static bool StructuredConsent(string title, IList<MstscDialogControl> controls)
        {
            if (Label(title) != "Remote Desktop Connection security warning") return false;
            bool publisher = false, type = false, remote = false;
            for (int i = 0; i < controls.Count; i++)
            {
                if (StaticField(controls, i, "Publisher") == "Unknown publisher") publisher = true;
                if (StaticField(controls, i, "Type") == "Remote Desktop Connection") type = true;
                if (StaticField(controls, i, "Remote computer") != null) remote = true;
            }
            return publisher && type && remote;
        }

        private static bool StructuredAddress(string displayed, string address)
        {
            if (displayed == address) return true;
            // Server 2022's file-consent UI displays the host without its port.
            // The exact schema-validated launch profile supplies that port; live
            // success still requires evidence from the launched fixture server.
            const string prefix = "127.0.0.1:";
            if (displayed != "127.0.0.1" || !address.StartsWith(prefix, StringComparison.Ordinal)) return false;
            string portText = address.Substring(prefix.Length);
            int port;
            return Int32.TryParse(portText, NumberStyles.None, CultureInfo.InvariantCulture, out port) &&
                port > 0 && port <= 65535 && port.ToString(CultureInfo.InvariantCulture) == portText;
        }

        internal static string Kind(string title, IList<MstscDialogControl> controls)
        {
            if (Label(title) == "Remote Desktop Connection" && HasText(controls, Education)) return "education";
            if (StructuredConsent(title, controls) ||
                (Label(title) == "Remote Desktop Connection security warning" && HasText(controls, Warning))) return "consent";
            if (Label(title) == "Remote Desktop Connection" && HasText(controls, LegacyWarning)) return "consent";
            return null;
        }

        internal static MstscDialogDecision Evaluate(string title, IList<MstscDialogControl> controls, string address)
        {
            var result = new MstscDialogDecision { Kind = Kind(title, controls), Reason = "unrecognized_dialog" };
            if (result.Kind == null) return result;
            bool endpoint = false, publisher = HasText(controls, "Unknown publisher");
            bool structured = StructuredConsent(title, controls);
            int checkCount = 0, buttonCount = 0;
            for (int i = 0; i < controls.Count; i++)
            {
                MstscDialogControl control = controls[i];
                if (control.Visible && control.Password)
                { result.Reason = "credential_field_present"; return result; }
                if (control.Kind == "CheckBox")
                {
                    checkCount++;
                    if (!control.Owned || control.Toggle < 0 || control.Toggle > 1)
                    { result.Reason = "checkbox_state_unavailable"; return result; }
                    if (result.Kind == "education")
                    {
                        if (Label(control.Name) != EducationalCheck || !control.Enabled || !control.Visible)
                        { result.Reason = "unexpected_education_checkbox"; return result; }
                        result.Check = i;
                    }
                    else if (control.Toggle != 0)
                    { result.Reason = "resource_redirection_selected"; return result; }
                }
                if (control.Kind == "Button" && Label(control.Name) == (result.Kind == "education" ? "OK" : "Connect"))
                {
                    buttonCount++;
                    // Education enables OK only after its checkbox is selected.
                    // Invoke rechecks the live button state after that selection.
                    // The connection-consent button must already be enabled.
                    if (!control.Owned || !control.Visible || !control.CanInvoke ||
                        (!control.Enabled && result.Kind != "education"))
                    { result.Reason = "consent_button_unavailable"; return result; }
                    result.Button = i;
                }
                if (!control.Owned || !control.Visible || control.Password) continue;
                if (control.Kind == "Text")
                {
                    string name = (control.Name ?? "").Trim();
                    if (!structured && name == address) endpoint = true;
                    if (structured && Field(name, "Remote computer"))
                    {
                        string displayed = StaticField(controls, i, "Remote computer");
                        if (displayed == null || !StructuredAddress(displayed, address))
                        { result.Reason = "remote_address_mismatch"; return result; }
                        endpoint = true;
                    }
                    foreach (string prefix in new[] { "Remote computer:", "&Remote computer:" })
                    {
                        if (!name.StartsWith(prefix, StringComparison.Ordinal)) continue;
                        string value = name.Substring(prefix.Length).Trim();
                        if (value.Length == 0) continue; // Separate static label and value.
                        if (value != address) { result.Reason = "remote_address_mismatch"; return result; }
                        endpoint = true;
                    }
                }
                // UIA Edit.Name is a label. The text of a read-only field lives
                // in ValuePattern, unlike a static Text control's Name property.
                if (control.Kind == "Edit" && control.ReadOnly && control.Value != null)
                {
                    if (Field(control.Label, "Remote computer"))
                    {
                        if (control.Value.Trim() != address) { result.Reason = "remote_address_mismatch"; return result; }
                        endpoint = true;
                    }
                    if (Field(control.Label, "Publisher") && control.Value.Trim() == "Unknown publisher") publisher = true;
                }
            }
            if (buttonCount != 1) { result.Reason = "consent_button_ambiguous_or_missing"; return result; }
            if (result.Kind == "education" && checkCount != 1)
            { result.Reason = "education_checkbox_ambiguous_or_missing"; return result; }
            if (result.Kind == "consent")
            {
                if (!endpoint) { result.Reason = "expected_remote_address_missing"; return result; }
                if (Label(title) == "Remote Desktop Connection security warning" && !publisher)
                { result.Reason = "unknown_publisher_missing"; return result; }
            }
            result.Reason = null;
            return result;
        }

        internal static string DiagnosticText(string text, string address)
        {
            string label = Label(text);
            foreach (string known in new[] { "Remote Desktop Connection", "Remote Desktop Connection security warning",
                "Windows Security", Education, EducationalCheck, Warning, LegacyWarning, "Unknown publisher",
                "Remote computer:", "Publisher:", "Type:", "Connect", "OK", "Cancel", "Yes", "No", "Show Details", "Hide Details",
                "Clipboard", "Printers", "Drives", "Smart cards", "Ports", "Devices", "WebAuthn" })
                if (label == known) return known;
            if ((text ?? "").Trim() == address) return "<expected-loopback-address>";
            if ((text ?? "").Trim() == "127.0.0.1") return "<loopback-host>";
            return String.IsNullOrEmpty(text) ? "" : "<redacted>";
        }
    }

    internal sealed class MstscDialogs
    {
        private sealed class Accepted
        {
            internal IntPtr Window;
            internal readonly Stopwatch Age = Stopwatch.StartNew();
        }
        private readonly Dictionary<string, Accepted> accepted = new Dictionary<string, Accepted>();
        private readonly HashSet<string> reported = new HashSet<string>();

        internal bool Handle(IntPtr window, uint processId, string address, Report report)
        {
            // A progress dialog can disappear during enumeration. A stale UIA
            // reference never authorizes a click.
            try { return HandlePresent(window, processId, address, report); }
            catch (ElementNotAvailableException) { return false; }
        }

        private bool HandlePresent(IntPtr window, uint processId, string address, Report report)
        {
            AutomationElement root = AutomationElement.FromHandle(window);
            if (root == null || root.Current.ProcessId != processId || MstscWindows.Owner(window) != processId) return false;
            string title = root.Current.Name;
            AutomationElementCollection elements = root.FindAll(TreeScope.Descendants, Condition.TrueCondition);
            if (elements.Count > 256)
            {
                if (reported.Add("control_limit")) report.Add("client", "recorded", "mstsc_dialog_rejected", new { reason = "control_limit" });
                return false;
            }
            var controls = new List<MstscDialogControl>();
            foreach (AutomationElement element in elements)
            {
                AutomationElement.AutomationElementInformation info = element.Current;
                string kind = info.ControlType.ProgrammaticName.Replace("ControlType.", "");
                var control = new MstscDialogControl { Kind = kind, Name = info.IsPassword ? "" : info.Name,
                    Owned = Owned(element, processId) || (info.ProcessId == 0 && kind == "Text"),
                    Enabled = info.IsEnabled, Visible = !info.IsOffscreen, Password = info.IsPassword };
                object pattern;
                control.CanInvoke = element.TryGetCurrentPattern(InvokePattern.Pattern, out pattern);
                if (kind == "CheckBox" && element.TryGetCurrentPattern(TogglePattern.Pattern, out pattern))
                    control.Toggle = (int)((TogglePattern)pattern).Current.ToggleState;
                controls.Add(control);
            }
            // Read values only inside a recognized file-consent dialog, and only
            // from owned, non-password, read-only endpoint/publisher fields.
            if (MstscDialogPolicy.Kind(title, controls) == "consent")
            {
                for (int i = 0; i < controls.Count; i++)
                {
                    MstscDialogControl control = controls[i];
                    if (control.Kind != "Edit" || !control.Owned || !control.Visible || control.Password) continue;
                    control.Label = control.Name;
                    AutomationElement label = elements[i].Current.LabeledBy;
                    if (label != null && (Owned(label, processId) || label.Current.ProcessId == 0) && !label.Current.IsPassword)
                        control.Label = label.Current.Name;
                    if (!MstscDialogPolicy.Field(control.Label, "Remote computer") && !MstscDialogPolicy.Field(control.Label, "Publisher")) continue;
                    object pattern;
                    if (!elements[i].TryGetCurrentPattern(ValuePattern.Pattern, out pattern)) continue;
                    var value = (ValuePattern)pattern;
                    control.ReadOnly = value.Current.IsReadOnly;
                    if (control.ReadOnly) control.Value = value.Current.Value;
                }
            }
            MstscDialogDecision decision = MstscDialogPolicy.Evaluate(title, controls, address);
            if (!decision.Allowed) return Rejected(title, controls, address, decision.Reason, report);
            Accepted previous;
            if (accepted.TryGetValue(decision.Kind, out previous))
            {
                if (previous.Window != window) return Rejected(title, controls, address, "repeated_consent_dialog", report);
                if (previous.Age.Elapsed.TotalSeconds > 5) return Rejected(title, controls, address, "accepted_dialog_did_not_close", report);
                return true;
            }
            if (decision.Check >= 0)
            {
                AutomationElement check = elements[decision.Check];
                object pattern;
                if (!Owned(check, processId) || !check.Current.IsEnabled || check.Current.IsOffscreen ||
                    !check.TryGetCurrentPattern(TogglePattern.Pattern, out pattern))
                    return Rejected(title, controls, address, "education_checkbox_changed", report);
                var toggle = (TogglePattern)pattern;
                if (toggle.Current.ToggleState == ToggleState.Off) toggle.Toggle();
                if (toggle.Current.ToggleState != ToggleState.On) return false;
            }
            if (!Invoke(elements[decision.Button], processId))
                return Rejected(title, controls, address, "consent_button_changed", report);
            accepted.Add(decision.Kind, new Accepted { Window = window });
            report.Add("client", "progress", decision.Kind == "education" ? "mstsc_rdp_file_education_accepted" :
                "mstsc_loopback_profile_consent_accepted", new { localFixture = true });
            return true;
        }

        private bool Rejected(string title, IList<MstscDialogControl> controls, string address, string reason, Report report)
        {
            if (reported.Count >= 16 || !reported.Add(reason)) return false;
            var diagnostics = new List<object>();
            foreach (MstscDialogControl control in controls)
                diagnostics.Add(new { kind = control.Kind, name = MstscDialogPolicy.DiagnosticText(control.Name, address),
                    label = MstscDialogPolicy.DiagnosticText(control.Label, address), owned = control.Owned,
                    enabled = control.Enabled, visible = control.Visible, password = control.Password,
                    readOnly = control.ReadOnly, hasValue = control.Value != null, expectedAddress = control.Value == address,
                    toggle = control.Toggle, canInvoke = control.CanInvoke });
            report.Add("client", "recorded", "mstsc_dialog_rejected", new {
                reason = reason, title = MstscDialogPolicy.DiagnosticText(title, address), controls = diagnostics });
            return false;
        }

        private static bool Owned(AutomationElement element, uint processId)
        {
            if (element.Current.ProcessId == processId) return true;
            int handle = element.Current.NativeWindowHandle;
            return element.Current.ProcessId == 0 && handle != 0 && MstscWindows.Owner(new IntPtr(handle)) == processId;
        }

        private static bool Invoke(AutomationElement button, uint processId)
        {
            object pattern;
            if (!Owned(button, processId) || !button.Current.IsEnabled || button.Current.IsOffscreen ||
                !button.TryGetCurrentPattern(InvokePattern.Pattern, out pattern)) return false;
            ((InvokePattern)pattern).Invoke();
            return true;
        }
    }

    internal static class MstscDialogTests
    {
        private const string Address = "127.0.0.1:3390";
        private const string ModernTitle = "Remote Desktop Connection security warning";
        private const string LegacyTitle = "Remote Desktop Connection";

        internal static void Run()
        {
            Accept("modern static text", ModernTitle, Consent(false), "consent", 3, -1);
            var legacy = Consent(false);
            legacy[0].Name = MstscDialogPolicy.LegacyWarning;
            legacy.RemoveAt(2);
            Accept("legacy static text without publisher field", LegacyTitle, legacy, "consent", 2, -1);
            var combined = Consent(false);
            combined[1].Name = "Remote computer: " + Address;
            Accept("combined static label and endpoint", ModernTitle, combined, "consent", 3, -1);
            var separate = Consent(false);
            separate.Add(Control("Text", "Remote computer:"));
            Accept("separate static label and endpoint", ModernTitle, separate, "consent", 3, -1);
            Accept("read-only endpoint and publisher values", ModernTitle, Consent(true), "consent", 3, -1);
            var accelerators = Consent(true);
            accelerators[1].Label = " &Remote computer: ";
            accelerators[2].Label = "Pub&lisher:";
            accelerators[3].Name = "&Connect";
            Accept("label accelerators", ModernTitle, accelerators, "consent", 3, -1);
            var redirectionOff = Consent(true);
            redirectionOff.Add(Checkbox("Clipboard", 0));
            Accept("redirection explicitly off", ModernTitle, redirectionOff, "consent", 3, -1);
            var hiddenRedirectionOff = Consent(true);
            var hiddenCheckbox = Checkbox("Clipboard", 0);
            hiddenCheckbox.Visible = false;
            hiddenRedirectionOff.Add(hiddenCheckbox);
            Accept("collapsed redirection explicitly off", ModernTitle, hiddenRedirectionOff, "consent", 3, -1);

            foreach (string wrong in new[] { "127.0.0.1:3391", "example.invalid:3390", Address + ".example.invalid", "127.0.0.&1:3390" })
            {
                var edit = Consent(true);
                edit[1].Value = wrong;
                Reject("read-only endpoint mismatch " + wrong, ModernTitle, edit, "remote_address_mismatch");
                var text = Consent(false);
                text[1].Name = "Remote computer: " + wrong;
                Reject("combined static endpoint mismatch " + wrong, ModernTitle, text, "remote_address_mismatch");
                var bare = Consent(false);
                bare[1].Name = wrong;
                Reject("bare static endpoint mismatch " + wrong, ModernTitle, bare, "expected_remote_address_missing");
            }
            RejectConsent("writable endpoint", delegate(List<MstscDialogControl> controls) { controls[1].ReadOnly = false; },
                "expected_remote_address_missing");
            RejectConsent("password endpoint", delegate(List<MstscDialogControl> controls) { controls[1].Password = true; },
                "credential_field_present");
            RejectConsent("password field in otherwise valid consent", delegate(List<MstscDialogControl> controls) {
                var password = Control("Edit", "Password");
                password.Password = true;
                controls.Add(password);
            }, "credential_field_present");
            RejectConsent("unowned endpoint", delegate(List<MstscDialogControl> controls) { controls[1].Owned = false; },
                "expected_remote_address_missing");
            RejectConsent("hidden endpoint", delegate(List<MstscDialogControl> controls) { controls[1].Visible = false; },
                "expected_remote_address_missing");
            RejectConsent("unrelated value", delegate(List<MstscDialogControl> controls) { controls[1].Label = "Username:"; },
                "expected_remote_address_missing");
            RejectConsent("label is not endpoint value", delegate(List<MstscDialogControl> controls) {
                controls[1].Name = Address; controls[1].Value = null;
            }, "expected_remote_address_missing");
            RejectConsent("missing banner", delegate(List<MstscDialogControl> controls) { controls.RemoveAt(0); },
                "unrecognized_dialog");
            RejectConsent("unowned banner", delegate(List<MstscDialogControl> controls) { controls[0].Owned = false; },
                "unrecognized_dialog");
            RejectConsent("missing publisher", delegate(List<MstscDialogControl> controls) { controls.RemoveAt(2); },
                "unknown_publisher_missing");
            RejectConsent("writable publisher", delegate(List<MstscDialogControl> controls) { controls[2].ReadOnly = false; },
                "unknown_publisher_missing");
            RejectConsent("wrong publisher", delegate(List<MstscDialogControl> controls) { controls[2].Value = "Example Publisher"; },
                "unknown_publisher_missing");
            RejectConsent("missing Connect", delegate(List<MstscDialogControl> controls) { controls.RemoveAt(3); },
                "consent_button_ambiguous_or_missing");
            RejectConsent("disabled Connect", delegate(List<MstscDialogControl> controls) { controls[3].Enabled = false; },
                "consent_button_unavailable");
            RejectConsent("hidden Connect", delegate(List<MstscDialogControl> controls) { controls[3].Visible = false; },
                "consent_button_unavailable");
            RejectConsent("unowned Connect", delegate(List<MstscDialogControl> controls) { controls[3].Owned = false; },
                "consent_button_unavailable");
            RejectConsent("non-invokable Connect", delegate(List<MstscDialogControl> controls) { controls[3].CanInvoke = false; },
                "consent_button_unavailable");
            RejectConsent("duplicate Connect", delegate(List<MstscDialogControl> controls) { controls.Add(Control("Button", "Connect")); },
                "consent_button_ambiguous_or_missing");
            foreach (int state in new[] { 1, 2, -1 })
            {
                var controls = Consent(true);
                controls.Add(Checkbox("Clipboard", state));
                Reject("redirection toggle " + state, ModernTitle, controls,
                    state == 1 ? "resource_redirection_selected" : "checkbox_state_unavailable");
            }
            RejectConsent("collapsed redirection selected", delegate(List<MstscDialogControl> controls) {
                var checkbox = Checkbox("Clipboard", 1);
                checkbox.Visible = false;
                controls.Add(checkbox);
            }, "resource_redirection_selected");
            var certificate = Consent(false);
            certificate[0].Name = "The identity of the remote computer cannot be verified. Do you want to connect anyway?";
            certificate[3].Name = "Yes";
            Reject("certificate warning", LegacyTitle, certificate, "unrecognized_dialog");
            var credential = Consent(true);
            credential[0].Name = "Enter your credentials";
            credential.Add(new MstscDialogControl { Kind = "Edit", Name = "Password", Password = true, Owned = true });
            Reject("credentials prompt", "Windows Security", credential, "unrecognized_dialog");

            // Windows Server 2022 exposes the file-consent identity as static
            // fields, omits the banner from Text controls, and shows only the
            // host in Remote computer even when the profile includes a port.
            Accept("observed structured consent with host only", ModernTitle,
                ModernStructuredConsent(), "consent", 8, -1);
            var structuredAddress = ModernStructuredConsent();
            structuredAddress[5].Name = Address;
            structuredAddress[6].Name = Address;
            Accept("structured consent with full address", ModernTitle, structuredAddress, "consent", 8, -1);
            foreach (string wrong in new[] { "127.0.0.2", "127.0.0.1:3391", "127.0.0.1.example.invalid",
                Address + ".example.invalid", "localhost", "127.0.0.&1" })
            {
                var controls = ModernStructuredConsent();
                controls[5].Name = wrong;
                controls[6].Name = wrong;
                Check("structured remote mismatch " + wrong,
                    !MstscDialogPolicy.Evaluate(ModernTitle, controls, Address).Allowed);
            }
            RejectStructuredConsent("missing remote label", delegate(List<MstscDialogControl> controls) {
                controls.RemoveAt(4);
            });
            RejectStructuredConsent("missing type label", delegate(List<MstscDialogControl> controls) {
                controls.RemoveAt(2);
            });
            RejectStructuredConsent("wrong type", delegate(List<MstscDialogControl> controls) {
                controls[3].Name = "Other connection";
            });
            RejectStructuredConsent("missing publisher label", delegate(List<MstscDialogControl> controls) {
                controls.RemoveAt(0);
            });
            RejectStructuredConsent("missing publisher value", delegate(List<MstscDialogControl> controls) {
                controls.RemoveAt(1);
            });
            RejectStructuredConsent("wrong publisher", delegate(List<MstscDialogControl> controls) {
                controls[1].Name = "Example Publisher";
            });
            RejectStructuredConsent("unrelated correct loopback text", delegate(List<MstscDialogControl> controls) {
                controls[5].Name = "127.0.0.2";
                controls[6].Name = "127.0.0.2";
                controls.Add(Control("Text", "127.0.0.1"));
                controls.Add(Control("Text", Address));
            });
            RejectStructuredConsent("conflicting remote fields", delegate(List<MstscDialogControl> controls) {
                controls.Add(Control("Text", "Remote computer:"));
                controls.Add(Control("Text", "127.0.0.1:3391"));
            });
            RejectStructuredConsent("hidden remote value", delegate(List<MstscDialogControl> controls) {
                controls[5].Visible = false;
            });
            RejectStructuredConsent("unowned remote value", delegate(List<MstscDialogControl> controls) {
                controls[5].Owned = false;
            });
            RejectStructuredConsent("hidden remote label", delegate(List<MstscDialogControl> controls) {
                controls[4].Visible = false;
            });
            RejectStructuredConsent("unowned remote label", delegate(List<MstscDialogControl> controls) {
                controls[4].Owned = false;
            });
            RejectStructuredConsent("password field", delegate(List<MstscDialogControl> controls) {
                var password = Control("Edit", "Password");
                password.Password = true;
                controls.Add(password);
            });
            RejectStructuredConsent("writable remote field", delegate(List<MstscDialogControl> controls) {
                controls[5] = Control("Edit", "Remote computer:");
                controls[5].Label = "Remote computer:";
                controls[5].Value = "127.0.0.1";
                controls[5].ReadOnly = false;
            });
            RejectStructuredConsent("missing Connect", delegate(List<MstscDialogControl> controls) {
                controls.RemoveAt(8);
            });
            RejectStructuredConsent("disabled Connect", delegate(List<MstscDialogControl> controls) {
                controls[8].Enabled = false;
            });
            RejectStructuredConsent("selected resource", delegate(List<MstscDialogControl> controls) {
                controls[7].Toggle = 1;
            });

            Accept("education initially unchecked", LegacyTitle, Education(), "education", 2, 1);
            // Windows Server 2022 CI exposes OK as disabled until this checkbox
            // is selected. Selection must be allowed before OK can be invoked.
            var gatedEducation = Education();
            gatedEducation[2].Enabled = false;
            Accept("education unchecked with disabled OK", LegacyTitle, gatedEducation, "education", 2, 1);
            var enablingEducation = Education();
            enablingEducation[1].Toggle = 1;
            enablingEducation[2].Enabled = false;
            Accept("education awaiting OK enablement", LegacyTitle, enablingEducation, "education", 2, 1);
            RejectEducation("education hidden OK", delegate(List<MstscDialogControl> controls) { controls[2].Visible = false; },
                "consent_button_unavailable");
            RejectEducation("education unowned OK", delegate(List<MstscDialogControl> controls) { controls[2].Owned = false; },
                "consent_button_unavailable");
            RejectEducation("education non-invokable OK", delegate(List<MstscDialogControl> controls) { controls[2].CanInvoke = false; },
                "consent_button_unavailable");
            var checkedEducation = Education();
            checkedEducation[1].Toggle = 1;
            Accept("education already checked", LegacyTitle, checkedEducation, "education", 2, 1);
            RejectEducation("education missing checkbox", delegate(List<MstscDialogControl> controls) { controls.RemoveAt(1); },
                "education_checkbox_ambiguous_or_missing");
            RejectEducation("education duplicate checkbox", delegate(List<MstscDialogControl> controls) {
                controls.Add(Checkbox(MstscDialogPolicy.EducationalCheck, 0));
            }, "education_checkbox_ambiguous_or_missing");
            RejectEducation("education unrelated checkbox", delegate(List<MstscDialogControl> controls) { controls[1].Name = "Remember me"; },
                "unexpected_education_checkbox");
            RejectEducation("education disabled checkbox", delegate(List<MstscDialogControl> controls) { controls[1].Enabled = false; },
                "unexpected_education_checkbox");
            RejectEducation("education hidden checkbox", delegate(List<MstscDialogControl> controls) { controls[1].Visible = false; },
                "unexpected_education_checkbox");
            foreach (int state in new[] { 2, -1 })
            {
                var controls = Education();
                controls[1].Toggle = state;
                Reject("education unavailable toggle " + state, LegacyTitle, controls, "checkbox_state_unavailable");
            }

            foreach (string secret in new[] { "secret-password", "DOMAIN\\private-user", "private.example.invalid:3389",
                "Remote computer: " + Address, "Publisher: private-person", "Connect secret-password", "127.0.0.&1:3390" })
                Check("diagnostics redact " + secret, MstscDialogPolicy.DiagnosticText(secret, Address) == "<redacted>");
            Check("diagnostics identify expected endpoint without exposing it",
                MstscDialogPolicy.DiagnosticText(Address, Address) == "<expected-loopback-address>");
            Check("diagnostics retain known label", MstscDialogPolicy.DiagnosticText("&Connect", Address) == "Connect");
            Check("diagnostics handle absent text", MstscDialogPolicy.DiagnosticText(null, Address) == "");
        }

        private static MstscDialogControl Control(string kind, string name)
        {
            return new MstscDialogControl { Kind = kind, Name = name, Owned = true, Enabled = true,
                Visible = true, CanInvoke = kind == "Button" };
        }

        private static MstscDialogControl Checkbox(string name, int state)
        {
            MstscDialogControl control = Control("CheckBox", name);
            control.Toggle = state;
            return control;
        }

        private static List<MstscDialogControl> Consent(bool edits)
        {
            var controls = new List<MstscDialogControl> { Control("Text", MstscDialogPolicy.Warning),
                Control("Text", Address), Control("Text", "Unknown publisher"), Control("Button", "Connect") };
            if (edits)
            {
                controls[1] = Control("Edit", "Remote computer:");
                controls[1].Label = "Remote computer:";
                controls[1].Value = Address;
                controls[1].ReadOnly = true;
                controls[2] = Control("Edit", "Publisher:");
                controls[2].Label = "Publisher:";
                controls[2].Value = "Unknown publisher";
                controls[2].ReadOnly = true;
            }
            return controls;
        }

        private static List<MstscDialogControl> ModernStructuredConsent()
        {
            return new List<MstscDialogControl> {
                Control("Text", "Publisher:"), Control("Text", "Unknown publisher"),
                Control("Text", "Type:"), Control("Text", "Remote Desktop Connection"),
                Control("Text", "Remote computer:"), Control("Text", "127.0.0.1"),
                Control("Text", "127.0.0.1"), Checkbox("Clipboard", 0),
                Control("Button", "Connect"), Control("Button", "Cancel") };
        }

        private static void RejectStructuredConsent(string name, Action<List<MstscDialogControl>> change)
        {
            var controls = ModernStructuredConsent();
            change(controls);
            Check("structured consent rejects " + name,
                !MstscDialogPolicy.Evaluate(ModernTitle, controls, Address).Allowed);
        }

        private static List<MstscDialogControl> Education()
        {
            return new List<MstscDialogControl> { Control("Text", MstscDialogPolicy.Education),
                Checkbox(MstscDialogPolicy.EducationalCheck, 0), Control("Button", "OK") };
        }

        private static void RejectConsent(string name, Action<List<MstscDialogControl>> change, string reason)
        {
            var controls = Consent(true);
            change(controls);
            Reject(name, ModernTitle, controls, reason);
        }

        private static void RejectEducation(string name, Action<List<MstscDialogControl>> change, string reason)
        {
            var controls = Education();
            change(controls);
            Reject(name, LegacyTitle, controls, reason);
        }

        private static void Accept(string name, string title, IList<MstscDialogControl> controls, string kind, int button, int check)
        {
            MstscDialogDecision result = MstscDialogPolicy.Evaluate(title, controls, Address);
            Check(name + " accepted: " + result.Reason, result.Allowed);
            Check(name + " recognized kind", result.Kind == kind);
            Check(name + " selected action", result.Button == button && result.Check == check);
        }

        private static void Reject(string name, string title, IList<MstscDialogControl> controls, string reason)
        {
            MstscDialogDecision result = MstscDialogPolicy.Evaluate(title, controls, Address);
            Check(name + " rejected", !result.Allowed);
            Check(name + " rejection reason: " + result.Reason, result.Reason == reason);
        }

        private static void Check(string name, bool condition)
        {
            if (!condition) throw new InvalidOperationException("mstsc_dialog_self_test_failed: " + name);
        }
    }

    internal static class MstscWindows
    {
        internal static List<IntPtr> TopLevels(uint processId)
        {
            var windows = new List<IntPtr>();
            MstscNative.EnumWindows(delegate(IntPtr window, IntPtr ignored) {
                if (Owner(window) == processId && MstscNative.IsWindowVisible(window)) windows.Add(window);
                return true;
            }, IntPtr.Zero);
            return windows;
        }

        internal static uint Owner(IntPtr window)
        {
            uint processId;
            MstscNative.GetWindowThreadProcessId(window, out processId);
            return processId;
        }

        internal static string Class(IntPtr window)
        {
            var value = new StringBuilder(256);
            MstscNative.GetClassName(window, value, value.Capacity);
            return value.ToString();
        }

        internal static object WindowDiagnostic(IntPtr window)
        {
            Rectangle rect = ClientBounds(window);
            return new { windowClass = Class(window), visible = MstscNative.IsWindowVisible(window),
                x = rect.X, y = rect.Y, width = rect.Width, height = rect.Height,
                onScreen = Screen.PrimaryScreen.Bounds.Contains(rect) };
        }

        internal static void RecordWindows(uint processId, IntPtr root, string output, Report report)
        {
            var records = new List<object>();
            foreach (IntPtr window in TopLevels(processId))
            {
                records.Add(WindowDiagnostic(window));
                MstscNative.EnumChildWindows(window, delegate(IntPtr child, IntPtr ignored) {
                    if (Owner(child) == processId && records.Count < 64) records.Add(WindowDiagnostic(child));
                    return true;
                }, IntPtr.Zero);
            }
            report.Add("render", "recorded", "mstsc_window_diagnostics", new { foundRoot = root != IntPtr.Zero, windows = records });
            if (root != IntPtr.Zero)
            {
                try { using (Bitmap capture = Capture(root)) capture.Save(Path.Combine(output, "mstsc-window.png"), ImageFormat.Png); }
                catch (SmokeFailure error) { report.Add("render", "recorded", error.Reason, null); }
            }
        }

        internal static IntPtr RenderWindow(IntPtr root, uint processId)
        {
            IntPtr result = IntPtr.Zero;
            int best = -1;
            Rectangle viewport = ClientBounds(root);
            MstscNative.EnumChildWindows(root, delegate(IntPtr window, IntPtr ignored) {
                if (Owner(window) != processId || !MstscNative.IsWindowVisible(window)) return true;
                string name = Class(window);
                int rank = name == "UIOutputClass" ? 3 : name == "UIContainerClass" ? 2 : name == "TscShellAxHostClass" ? 1 : -1;
                Rectangle bounds = ClientBounds(window);
                if (rank > best && bounds.Width >= 640 && bounds.Height >= 480 &&
                    viewport.Contains(bounds) && Screen.PrimaryScreen.Bounds.Contains(bounds))
                { result = window; best = rank; }
                return true;
            }, IntPtr.Zero);
            return result;
        }

        internal static Rectangle ClientBounds(IntPtr window)
        {
            MstscNative.Rect rect;
            var point = new MstscNative.Point();
            if (!MstscNative.GetClientRect(window, out rect) || !MstscNative.ClientToScreen(window, ref point)) return Rectangle.Empty;
            return new Rectangle(point.X, point.Y, rect.Right - rect.Left, rect.Bottom - rect.Top);
        }

        internal static Rectangle WindowBounds(IntPtr window)
        {
            MstscNative.Rect rect;
            if (!MstscNative.GetWindowRect(window, out rect))
                throw new SmokeFailure("resize", "mstsc_window_bounds_unavailable");
            return Rectangle.FromLTRB(rect.Left, rect.Top, rect.Right, rect.Bottom);
        }

        internal static void Resize(IntPtr window, uint processId, Rectangle bounds)
        {
            if (Owner(window) != processId || Class(window) != "TscShellContainerClass" ||
                !Screen.PrimaryScreen.WorkingArea.Contains(bounds))
                throw new SmokeFailure("resize", "invalid_mstsc_resize_target");
            if (!MstscNative.SetWindowPos(window, IntPtr.Zero, bounds.X, bounds.Y, bounds.Width, bounds.Height, 0x0044))
                throw new SmokeFailure("resize", "mstsc_window_resize_failed");
            MstscNative.SetForegroundWindow(window);
        }

        internal static Bitmap Capture(IntPtr window)
        {
            Rectangle bounds = ClientBounds(window);
            if (bounds.Width < 640 || bounds.Height < 480 || !Screen.PrimaryScreen.Bounds.Contains(bounds))
                throw new SmokeFailure("infrastructure", "mstsc_viewport_not_visible");
            var result = new Bitmap(bounds.Width, bounds.Height, PixelFormat.Format24bppRgb);
            try
            {
                using (Graphics graphics = Graphics.FromImage(result))
                    graphics.CopyFromScreen(bounds.Location, Point.Empty, bounds.Size, CopyPixelOperation.SourceCopy);
                return result;
            }
            catch { result.Dispose(); throw; }
        }

        internal static bool FocusAllowed(uint expected, uint foreground, uint focused, bool sameRoot, string focusedClass)
        {
            return expected != 0 && foreground == expected && focused == expected && sameRoot &&
                (focusedClass == "UIInputClass" || focusedClass == "IHWindowClass");
        }

        internal static void SendF8(IntPtr root, uint processId, Report report)
        {
            Native.RequireInteractiveDesktop();
            MstscNative.SetForegroundWindow(root);
            IntPtr foreground = MstscNative.GetForegroundWindow();
            uint ignored;
            uint thread = MstscNative.GetWindowThreadProcessId(foreground, out ignored);
            var info = new MstscNative.GuiThreadInfo();
            info.Size = Marshal.SizeOf(info);
            bool queried = MstscNative.GetGUIThreadInfo(thread, ref info);
            bool sameRoot = MstscNative.GetAncestor(foreground, 2) == root && MstscNative.GetAncestor(info.Focus, 2) == root;
            string focusedClass = Class(info.Focus);
            report.Add("input", "recorded", "mstsc_input_focus", new { queried = queried,
                foregroundOwned = Owner(foreground) == processId, focusedOwned = Owner(info.Focus) == processId,
                sameRoot = sameRoot, focusedClass = focusedClass });
            if (!queried || !FocusAllowed(processId, Owner(foreground), Owner(info.Focus), sameRoot, focusedClass))
                throw new SmokeFailure("infrastructure", "mstsc_render_control_not_focused");
            // Scan-code input goes through the actual foreground Windows client.
            // Send both edges in one atomic insertion; never type credentials.
            var input = new MstscNative.Input[2];
            input[0].Type = input[1].Type = 1;
            input[0].Data.Keyboard.Scan = input[1].Data.Keyboard.Scan = 0x42;
            input[0].Data.Keyboard.Flags = 0x0008;
            input[1].Data.Keyboard.Flags = 0x0008 | 0x0002;
            if (MstscNative.SendInput(2, input, Marshal.SizeOf(typeof(MstscNative.Input))) != 2)
                throw new SmokeFailure("input", "mstsc_sendinput_failed");
        }
    }

    internal static class MstscProgram
    {
        [STAThread]
        private static int Main(string[] args)
        {
            Report report = null;
            MstscProcess client = null;
            int result = 1;
            try
            {
                if (args.Length == 1 && args[0] == "--self-test") return SelfTest();
                if (args.Length == 2 && args[0] == "--check-profile")
                { MstscProfile.Parse(File.ReadAllText(args[1])); return 0; }
                if (args.Length != 0) throw new SmokeFailure("configuration", "unexpected_arguments");
                string output = Environment.GetEnvironmentVariable("RDP_SMOKE_OUTPUT");
                string profilePath = Environment.GetEnvironmentVariable("RDP_SMOKE_PROFILE");
                if (String.IsNullOrWhiteSpace(output) || String.IsNullOrWhiteSpace(profilePath))
                    throw new SmokeFailure("configuration", "profile_and_output_required");
                Directory.CreateDirectory(output);
                report = new Report(output);
                report.Add("client", "started", null, new { harness = "Microsoft mstsc.exe", schema = 1 });
                report.Add("environment", "recorded", null, Native.RuntimeMetadata());
                if (Environment.GetEnvironmentVariable("RDP_SMOKE_LOCAL") != "1")
                    throw new SmokeFailure("configuration", "mstsc_requires_local_fixture");
                string address = MstscProfile.Parse(File.ReadAllText(profilePath));
                profilePath = Path.GetFullPath(profilePath);
                // Nothing inherited by mstsc should contain the ActiveX fixture's
                // password or a caller's external gateway test password.
                Environment.SetEnvironmentVariable("RDP_SMOKE_PASSWORD", null);
                Environment.SetEnvironmentVariable("RDP_TEST_PASSWORD", null);
                Native.RequireInteractiveDesktop();
                Native.SetProcessDPIAware();
                Rectangle screen = Screen.PrimaryScreen.WorkingArea;
                if (screen.Width < 800 || screen.Height < 600) throw new SmokeFailure("infrastructure", "desktop_too_small");
                using (var watchdog = new System.Threading.Timer(delegate {
                    try { report.Add("infrastructure", "failed", "mstsc_watchdog_timeout", null); } catch { }
                    Environment.Exit(2); // Closing the job handle also kills mstsc.
                }, null, 120000, Timeout.Infinite))
                {
                    client = new MstscProcess(profilePath);
                    report.Add("client", "passed", "mstsc_process_started", new { client = "mstsc.exe", processId = client.Id });
                    Observe(client, address, output, report);
                }
                result = 0;
            }
            catch (SmokeFailure error)
            {
                if (report != null) report.Add(error.Stage, "failed", error.Reason, null);
                else Console.WriteLine("configuration: failed");
            }
            catch (Exception error)
            {
                if (report != null) report.Add("client", "failed", "mstsc_operation_failed", new { hresult = error.HResult });
                else Console.WriteLine("client: failed");
            }
            finally
            {
                if (client != null)
                {
                    try { client.Stop(report); }
                    catch (SmokeFailure error) { result = 1; if (report != null) report.Add(error.Stage, "failed", error.Reason, null); }
                    finally { client.Dispose(); }
                }
            }
            return result;
        }

        private static void Observe(MstscProcess client, string address, string output, Report report)
        {
            var clock = Stopwatch.StartNew();
            var state = new MstscObservation();
            var dialogs = new MstscDialogs();
            IntPtr positioned = IntPtr.Zero;
            Rectangle original = Rectangle.Empty, desired = Rectangle.Empty, beforeResize = Rectangle.Empty;
            int reportedResize = 0;
            bool diagnosed = false;
            double unknownDialogAt = -1, resizeRequestedAt = 0;
            while (clock.Elapsed.TotalSeconds < 90)
            {
                if (!client.Running) throw new SmokeFailure("connection", "mstsc_process_exited_early");
                Native.RequireInteractiveDesktop();
                IntPtr root = IntPtr.Zero;
                bool unknownDialog = false;
                foreach (IntPtr window in MstscWindows.TopLevels(client.Id))
                {
                    string name = MstscWindows.Class(window);
                    if (name == "TscShellContainerClass") { root = window; continue; }
                    if (name == "#32770")
                    {
                        if (state.InitialLayout >= 0 || !dialogs.Handle(window, client.Id, address, report))
                            unknownDialog = true;
                    }
                }
                double now = clock.Elapsed.TotalSeconds;
                if (unknownDialog)
                {
                    if (unknownDialogAt < 0) unknownDialogAt = now;
                    if (now - unknownDialogAt > 5) throw new SmokeFailure("client", "unexpected_mstsc_dialog");
                }
                else unknownDialogAt = -1;
                bool previouslyVisible = state.InitialLayout >= 0, previouslyConfirmed = state.InputConfirmed;
                bool observed = false;
                if (root != IntPtr.Zero && !unknownDialog)
                {
                    if (positioned == IntPtr.Zero)
                    {
                        Rectangle area = Screen.PrimaryScreen.WorkingArea;
                        original = desired = new Rectangle(area.Location, new Size(Math.Min(area.Width, 1100), Math.Min(area.Height, 850)));
                        MstscResize.Smaller(original.Size);
                        MstscWindows.Resize(root, client.Id, desired);
                        positioned = root;
                    }
                    if (positioned != root) throw new SmokeFailure("connection", "mstsc_window_replaced");
                    Rectangle actual = MstscWindows.WindowBounds(root);
                    if (actual != desired && state.Phase > 0 && now - resizeRequestedAt > 3)
                    {
                        MstscWindows.RecordWindows(client.Id, root, output, report);
                        throw new SmokeFailure("resize", "mstsc_window_size_not_applied");
                    }
                    IntPtr render = actual == desired ? MstscWindows.RenderWindow(root, client.Id) : IntPtr.Zero;
                    if (render != IntPtr.Zero)
                    {
                        using (Bitmap capture = MstscWindows.Capture(render))
                        {
                            Rectangle bounds = MstscPixels.Bounds(capture);
                            if (!bounds.IsEmpty)
                            {
                                using (Bitmap image = MstscPixels.Normalize(capture, bounds))
                                {
                                    int layout = Fixture.Match(Fixture.Samples(image));
                                    int marker = Fixture.Heartbeat(image);
                                    bool complete = state.Observe(layout, marker, now);
                                    observed = true;
                                    if (!previouslyVisible && state.InitialLayout >= 0)
                                    {
                                        image.Save(Path.Combine(output, "before-input.png"), ImageFormat.Png);
                                        report.Add("render", "passed", "fixture_visible", new { layout = layout,
                                            renderClass = MstscWindows.Class(render), visibleWidth = bounds.Width, visibleHeight = bounds.Height });
                                    }
                                    if (state.ReadyForInput)
                                    {
                                        MstscWindows.SendF8(root, client.Id, report);
                                        state.SubmittedInput(now);
                                        report.Add("input", "progress", "f8_submitted", new { phase = state.PhaseName,
                                            method = "SendInput", keyEncoding = "scan_code", controlFocused = true });
                                    }
                                    if (state.Phase == 0 && !previouslyConfirmed && state.InputConfirmed)
                                    {
                                        image.Save(Path.Combine(output, "after-input.png"), ImageFormat.Png);
                                        report.Add("input", "passed", "fixture_changed", new { layout = layout });
                                    }
                                    if (state.Phase > reportedResize && state.PhaseVerified(now))
                                    {
                                        capture.Save(Path.Combine(output, "resize-" + state.PhaseName + ".png"), ImageFormat.Png);
                                        report.Add("resize", "passed", "window_resize_verified", new {
                                            phase = state.PhaseName, beforeWidth = beforeResize.Width, beforeHeight = beforeResize.Height,
                                            windowWidth = actual.Width, windowHeight = actual.Height,
                                            visibleWidth = bounds.Width, visibleHeight = bounds.Height,
                                            layoutBefore = state.LayoutBefore, layoutAfter = layout,
                                            heartbeatChanges = state.PhaseHeartbeat.Changes,
                                            lastChangeAgeSeconds = now - state.PhaseHeartbeat.LastChangeAt });
                                        reportedResize = state.Phase;
                                    }
                                    if (complete)
                                    {
                                        image.Save(Path.Combine(output, "terminal.png"), ImageFormat.Png);
                                        report.Add("render", "passed", "heartbeat_verified", new {
                                            changes = state.Heartbeat.Changes, lastChangeAgeSeconds = now - state.Heartbeat.LastChangeAt });
                                        report.Add("session", "passed", "render_input_and_duration_verified", new {
                                            visibleSeconds = now - state.FirstFrameAt, requiredSeconds = 30 });
                                        return;
                                    }
                                    if (state.Phase < 2 && state.PhaseVerified(now))
                                    {
                                        beforeResize = actual;
                                        desired = state.Phase == 0 ? new Rectangle(original.Location, MstscResize.Smaller(original.Size)) : original;
                                        MstscWindows.Resize(root, client.Id, desired);
                                        state.BeginResize(now);
                                        resizeRequestedAt = now;
                                        report.Add("resize", "progress", "window_resize_requested", new {
                                            phase = state.PhaseName, beforeWidth = actual.Width, beforeHeight = actual.Height,
                                            requestedWidth = desired.Width, requestedHeight = desired.Height });
                                    }
                                }
                            }
                        }
                    }
                }
                if (!diagnosed && now >= 10 && state.InitialLayout < 0)
                {
                    MstscWindows.RecordWindows(client.Id, root, output, report);
                    diagnosed = true;
                }
                if (!observed) state.Observe(-1, -1, now);
                Thread.Sleep(250);
            }
            throw new SmokeFailure("session", "mstsc_session_timeout");
        }

        private static int SelfTest()
        {
            string profile = "full address:s:127.0.0.1:3390\r\nusername:s:rdp-smoke\r\nscreen mode id:i:1\r\nuse multimon:i:0\r\n" +
                "desktopwidth:i:1024\r\ndesktopheight:i:768\r\nsession bpp:i:32\r\nauthentication level:i:1\r\n" +
                "enablecredsspsupport:i:0\r\nnegotiate security layer:i:1\r\nprompt for credentials:i:0\r\n" +
                "prompt for credentials on client:i:0\r\nautoreconnection enabled:i:0\r\nredirectclipboard:i:0\r\n" +
                "redirectprinters:i:0\r\nredirectcomports:i:0\r\nredirectsmartcards:i:0\r\nredirectwebauthn:i:0\r\ndrivestoredirect:s:\r\n" +
                "audiomode:i:2\r\naudiocapturemode:i:0\r\nsmart sizing:i:1\r\ndynamic resolution:i:0\r\ndisableconnectionsharing:i:1\r\n";
            Check(MstscProfile.Parse(profile) == "127.0.0.1:3390");
            foreach (string bad in new[] { profile + "username:s:rdp-smoke\r\n", profile + "gatewayhostname:s:evil.invalid\r\n",
                profile.Replace("127.0.0.1", "localhost"), profile.Replace("3390", "03390"), profile.Replace("3390", "65536"),
                profile.Replace("authentication level:i:1", "authentication level:i:0"),
                profile.Replace("redirectclipboard:i:0", "redirectclipboard:i:1"), profile.Replace("s:rdp-smoke", "s:someone"),
                profile.Replace("enablecredsspsupport:i:0", "enablecredsspsupport:i:1"), profile + "\0" })
                Reject(delegate { MstscProfile.Parse(bad); });
            Check(MstscProcess.Quote("C:\\some directory\\client.rdp") == "\"C:\\some directory\\client.rdp\"");
            Reject(delegate { MstscProcess.Quote("bad\" /v:elsewhere"); });
            MstscDialogTests.Run();
            Check(MstscWindows.FocusAllowed(5, 5, 5, true, "UIInputClass"));
            Check(MstscWindows.FocusAllowed(5, 5, 5, true, "IHWindowClass"));
            Check(!MstscWindows.FocusAllowed(5, 6, 5, true, "IHWindowClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 6, true, "IHWindowClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 5, false, "IHWindowClass"));
            Check(!MstscWindows.FocusAllowed(5, 6, 5, true, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 6, true, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 5, false, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 5, true, "Edit"));
            ObservationTests();
            Check(MstscResize.Smaller(new Size(1024, 728)) == new Size(800, 600));
            Check(MstscResize.Smaller(new Size(800, 600)) == new Size(720, 540));
            Reject(delegate { MstscResize.Smaller(new Size(700, 540)); });
            foreach (Size size in new[] { new Size(1024, 768), new Size(1024, 705), new Size(960, 720), new Size(800, 600),
                new Size(800, 577), new Size(720, 517) })
            foreach (int layout in new[] { 0, 1 })
            foreach (int marker in new[] { 0, 1 })
            using (var image = new Bitmap(size.Width + 24, size.Height + 12))
            {
                using (Graphics graphics = Graphics.FromImage(image))
                {
                    graphics.Clear(Color.Black);
                    for (int i = 0; i < 4; i++) using (var brush = new SolidBrush(Fixture.Colors[Fixture.Layouts[layout][i]]))
                    {
                        int x = i % 2 * size.Width / 2, y = i / 2 * size.Height / 2;
                        int width = i % 2 == 0 ? size.Width / 2 : size.Width - size.Width / 2;
                        int height = i < 2 ? size.Height / 2 : size.Height - size.Height / 2;
                        graphics.FillRectangle(brush, 12 + x, 6 + y, width, height);
                    }
                    graphics.FillRectangle(marker == 0 ? Brushes.Black : Brushes.Red, 12, 6, size.Width / 64, size.Height / 48);
                }
                Rectangle bounds = MstscPixels.Bounds(image);
                Check(bounds == new Rectangle(12, 6, size.Width, size.Height));
                using (Bitmap normalized = MstscPixels.Normalize(image, bounds))
                { Check(Fixture.Match(Fixture.Samples(normalized)) == layout); Check(Fixture.Heartbeat(normalized) == marker); }
                if (size == new Size(1024, 768))
                {
                    foreach (Size croppedSize in new[] { new Size(1024, 705), new Size(960, 768) })
                    using (Bitmap cropped = image.Clone(new Rectangle(12, 6, croppedSize.Width, croppedSize.Height),
                        PixelFormat.Format24bppRgb))
                        Check(MstscPixels.Bounds(cropped).IsEmpty);
                }
            }
            using (var empty = new Bitmap(1024, 768)) Check(MstscPixels.Bounds(empty).IsEmpty);
            Check(Marshal.SizeOf(typeof(MstscNative.Input)) == (IntPtr.Size == 8 ? 40 : 28));
            Console.WriteLine("mstsc.exe harness self-tests passed");
            return 0;
        }

        private static void ObservationTests()
        {
            var state = new MstscObservation();
            state.Observe(0, 0, 0); state.Observe(0, 0, 0.25); state.Observe(-1, -1, 0.5);
            state.Observe(0, 0, 0.75); state.Observe(1, 0, 1); state.Observe(1, 0, 1.25);
            Check(state.InitialLayout < 0 && !state.ReadyForInput);
            state.Observe(1, 0, 1.5);
            Check(state.InitialLayout == 1 && state.ReadyForInput && !state.InputConfirmed);
            state.Observe(0, 0, 1.75);
            Check(!state.InputConfirmed && !state.ReadyForInput); // A layout alone cannot stand in for submitted input.
            Reject(delegate { state.SubmittedInput(1.75); });
            state.Observe(1, 0, 1.9);
            state.SubmittedInput(1.9);
            Reject(delegate { state.SubmittedInput(1.9); });
            for (int i = 2; i <= 35; i++) Check(!state.Observe(0, i % 2, i));
            Check(state.InputConfirmed && state.PhaseVerified(35)); // Duration without both resizes cannot pass.
            state.BeginResize(35);
            Check(!state.ReadyForInput && !state.InputConfirmed && state.PhaseHeartbeat.Changes == 0);
            Reject(delegate { state.BeginResize(35); });
            Reject(delegate { state.SubmittedInput(35); });
            state.Observe(-1, -1, 35.25); // Wrong window geometry is an invalid observation.
            state.Observe(0, 0, 35.5); state.Observe(0, 0, 35.75); state.Observe(0, 0, 36);
            Check(state.ReadyForInput && !state.PhaseVerified(36));
            state.SubmittedInput(36);
            state.Observe(0, 1, 36.25);
            Check(!state.InputConfirmed); // The old input confirmation must not survive a resize.
            state.Observe(1, 0, 36.5);
            Check(state.InputConfirmed && !state.PhaseVerified(36.5)); // Fresh heartbeat changes are also required.
            for (int i = 37; i <= 39; i++) Check(!state.Observe(1, i % 2, i));
            Check(state.Phase == 1 && state.PhaseVerified(39));
            state.BeginResize(39);
            state.Observe(1, 0, 39.25); state.Observe(1, 0, 39.5); state.Observe(1, 0, 39.75);
            Check(state.ReadyForInput && !state.InputConfirmed && state.PhaseHeartbeat.Changes == 0);
            state.SubmittedInput(39.75);
            for (int i = 40; i < 44; i++) Check(!state.Observe(0, i % 2, i));
            Check(state.Observe(0, 0, 44));
            Check(!state.Observe(-1, -1, 44.25)); // A stale successful phase cannot complete on a missing frame.
            Reject(delegate { state.BeginResize(44.25); });

            var early = VisibleObservation();
            early.SubmittedInput(0.5);
            for (int i = 1; i <= 5; i++) Check(!early.Observe(1, i % 2, i));
            early.BeginResize(5);
            early.Observe(1, 0, 5.25); early.Observe(1, 0, 5.5); early.Observe(1, 0, 5.75);
            early.SubmittedInput(5.75);
            for (int i = 6; i <= 10; i++) Check(!early.Observe(0, i % 2, i));
            early.BeginResize(10);
            early.Observe(0, 0, 10.25); early.Observe(0, 0, 10.5); early.Observe(0, 0, 10.75);
            early.SubmittedInput(10.75);
            for (int i = 11; i <= 30; i++) Check(!early.Observe(1, i % 2, i));
            Check(early.Observe(1, 1, 30.5)); // Successful resizes cannot shorten the visible session.

            var stalled = VisibleObservation();
            stalled.SubmittedInput(0.5);
            Reject(delegate { stalled.Observe(1, 0, 6); });
            var lost = VisibleObservation();
            Reject(delegate { lost.Observe(-1, 1, 4); });
            var noInput = VisibleObservation();
            noInput.SubmittedInput(0.5);
            for (int i = 1; i <= 10; i++) noInput.Observe(0, i % 2, i);
            Reject(delegate { noInput.Observe(0, 1, 11); });
            var resizedLost = VisibleObservation();
            resizedLost.SubmittedInput(0.5);
            for (int i = 1; i <= 5; i++) resizedLost.Observe(1, i % 2, i);
            resizedLost.BeginResize(5);
            Reject(delegate { resizedLost.Observe(-1, -1, 8.25); });
            Reject(delegate { new MstscObservation().Observe(-1, -1, 46); });
        }

        private static MstscObservation VisibleObservation()
        {
            var state = new MstscObservation();
            state.Observe(0, 0, 0); state.Observe(0, 0, 0.25); state.Observe(0, 0, 0.5);
            return state;
        }

        private static void Check(bool value) { if (!value) throw new InvalidOperationException("mstsc_self_test_failed"); }
        private static void Reject(Action action)
        {
            bool rejected = false;
            try { action(); } catch (SmokeFailure) { rejected = true; }
            Check(rejected);
        }
    }

    internal static class MstscNative
    {
        internal delegate bool EnumWindow(IntPtr window, IntPtr parameter);
        [StructLayout(LayoutKind.Sequential)] internal struct Point { internal int X, Y; }
        [StructLayout(LayoutKind.Sequential)] internal struct Rect { internal int Left, Top, Right, Bottom; }
        [StructLayout(LayoutKind.Sequential)] internal struct GuiThreadInfo
        {
            internal int Size, Flags;
            internal IntPtr Active, Focus, Capture, MenuOwner, MoveSize, Caret;
            internal Rect CaretRect;
        }
        [StructLayout(LayoutKind.Sequential)] internal struct KeyboardInput
        { internal ushort VirtualKey, Scan; internal uint Flags, Time; internal UIntPtr ExtraInfo; }
        [StructLayout(LayoutKind.Sequential)] internal struct MouseInput
        { internal int X, Y; internal uint Data, Flags, Time; internal UIntPtr ExtraInfo; }
        [StructLayout(LayoutKind.Explicit)] internal struct InputUnion
        { [FieldOffset(0)] internal KeyboardInput Keyboard; [FieldOffset(0)] internal MouseInput Mouse; }
        [StructLayout(LayoutKind.Sequential)] internal struct Input { internal uint Type; internal InputUnion Data; }
        [StructLayout(LayoutKind.Sequential)] internal struct BasicJobLimits
        {
            internal long ProcessTime, JobTime;
            internal uint LimitFlags;
            internal UIntPtr MinimumWorkingSet, MaximumWorkingSet;
            internal uint ActiveProcessLimit;
            internal UIntPtr Affinity;
            internal uint PriorityClass, SchedulingClass;
        }
        [StructLayout(LayoutKind.Sequential)] internal struct IoCounters
        { internal ulong ReadCount, WriteCount, OtherCount, ReadBytes, WriteBytes, OtherBytes; }
        [StructLayout(LayoutKind.Sequential)] internal struct JobLimits
        {
            internal BasicJobLimits Basic;
            internal IoCounters Io;
            internal UIntPtr ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory;
        }
        [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] internal struct StartupInfo
        {
            internal int Size;
            internal string Reserved, Desktop, Title;
            internal uint X, Y, Width, Height, CharacterWidth, CharacterHeight, Fill, Flags;
            internal ushort ShowWindow, ReservedSize;
            internal IntPtr ReservedBytes, StandardInput, StandardOutput, StandardError;
        }
        [StructLayout(LayoutKind.Sequential)] internal struct StartupInfoEx { internal StartupInfo Startup; internal IntPtr Attributes; }
        [StructLayout(LayoutKind.Sequential)] internal struct ProcessInfo { internal IntPtr Process, Thread; internal uint ProcessId, ThreadId; }

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] internal static extern IntPtr CreateJobObject(IntPtr security, string name);
        [DllImport("kernel32.dll", SetLastError = true)] internal static extern bool SetInformationJobObject(IntPtr job, int informationClass, ref JobLimits information, uint size);
        [DllImport("kernel32.dll", SetLastError = true)] internal static extern bool InitializeProcThreadAttributeList(IntPtr list, int count, int flags, ref IntPtr size);
        [DllImport("kernel32.dll", SetLastError = true)] internal static extern bool UpdateProcThreadAttribute(IntPtr list, uint flags, IntPtr attribute, IntPtr value, IntPtr size, IntPtr previous, IntPtr returned);
        [DllImport("kernel32.dll")] internal static extern void DeleteProcThreadAttributeList(IntPtr list);
        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] internal static extern bool CreateProcess(string application, StringBuilder command,
            IntPtr processSecurity, IntPtr threadSecurity, bool inheritHandles, uint flags, IntPtr environment, string directory, ref StartupInfoEx startup, out ProcessInfo process);
        [DllImport("kernel32.dll")] internal static extern bool CloseHandle(IntPtr handle);
        [DllImport("kernel32.dll")] internal static extern uint WaitForSingleObject(IntPtr handle, uint milliseconds);
        [DllImport("kernel32.dll")] internal static extern bool GetExitCodeProcess(IntPtr process, out uint exitCode);
        [DllImport("user32.dll")] internal static extern bool EnumWindows(EnumWindow callback, IntPtr parameter);
        [DllImport("user32.dll")] internal static extern bool EnumChildWindows(IntPtr parent, EnumWindow callback, IntPtr parameter);
        [DllImport("user32.dll")] internal static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
        [DllImport("user32.dll")] internal static extern bool IsWindowVisible(IntPtr window);
        [DllImport("user32.dll", CharSet = CharSet.Unicode)] internal static extern int GetClassName(IntPtr window, StringBuilder value, int maximum);
        [DllImport("user32.dll")] internal static extern bool GetClientRect(IntPtr window, out Rect rectangle);
        [DllImport("user32.dll")] internal static extern bool ClientToScreen(IntPtr window, ref Point point);
        [DllImport("user32.dll")] internal static extern bool GetWindowRect(IntPtr window, out Rect rect);
        [DllImport("user32.dll")] internal static extern bool SetWindowPos(IntPtr window, IntPtr after, int x, int y, int width, int height, uint flags);
        [DllImport("user32.dll")] internal static extern bool SetForegroundWindow(IntPtr window);
        [DllImport("user32.dll")] internal static extern IntPtr GetForegroundWindow();
        [DllImport("user32.dll")] internal static extern IntPtr GetAncestor(IntPtr window, uint flags);
        [DllImport("user32.dll")] internal static extern bool GetGUIThreadInfo(uint thread, ref GuiThreadInfo information);
        [DllImport("user32.dll", SetLastError = true)] internal static extern uint SendInput(uint count, Input[] inputs, int size);
    }
}
