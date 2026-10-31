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
        private int candidate = -1, samples;
        internal int InitialLayout = -1;
        internal bool InputConfirmed;
        internal double FirstFrameAt, LastGoodAt;
        internal readonly HeartbeatMonitor Heartbeat = new HeartbeatMonitor();

        internal bool Observe(int layout, int marker, double now)
        {
            if (InitialLayout < 0)
            {
                if (layout < 0) { candidate = -1; samples = 0; }
                else if (candidate != layout) { candidate = layout; samples = 1; }
                else samples++;
                if (samples < 3)
                {
                    if (now > 45) throw new SmokeFailure("render", "fixture_not_visible");
                    return false;
                }
                InitialLayout = layout;
                FirstFrameAt = LastGoodAt = now;
            }
            Heartbeat.Observe(marker, now);
            if (Heartbeat.Stalled(now, FirstFrameAt)) throw new SmokeFailure("render", "heartbeat_stalled");
            if (layout == 1 - InitialLayout) InputConfirmed = true;
            if (!InputConfirmed && now - FirstFrameAt > 10) throw new SmokeFailure("input", "fixture_did_not_change");
            if (layout == (InputConfirmed ? 1 - InitialLayout : InitialLayout)) LastGoodAt = now;
            if (now - LastGoodAt > 3) throw new SmokeFailure("render", "fixture_lost");
            return InputConfirmed && layout == 1 - InitialLayout && now - FirstFrameAt >= 30 && Heartbeat.Verified(now);
        }
    }

    internal static class MstscPixels
    {
        // Smart sizing can letterbox a 1024x768 session on the hosted 1024x768
        // desktop, where window chrome consumes space. Locate the fixture's four
        // color edges inside the owned render surface, then normalize only that
        // rectangle. The marker still represents displayed screen pixels.
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
            if (result.Width < 640 || result.Height < 480 || Math.Abs(result.Width * 3 - result.Height * 4) > 16)
                return Rectangle.Empty;
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

    internal static class MstscDialogs
    {
        private const string EducationalCheck = "I understand and allow RDP files to open on this device for my account";

        internal static bool HasEndpoint(IList<string> names, string address)
        {
            foreach (string name in names)
                if (name == address || name == "Remote computer: " + address) return true;
            return false;
        }

        internal static bool Handle(IntPtr window, uint processId, string address, Report report)
        {
            // A connection-progress dialog can disappear between enumeration
            // and UIA inspection. No action was authorized by that stale handle.
            try { return HandlePresent(window, processId, address, report); }
            catch (ElementNotAvailableException) { return false; }
        }

        private static bool HandlePresent(IntPtr window, uint processId, string address, Report report)
        {
            AutomationElement root = AutomationElement.FromHandle(window);
            if (root == null || root.Current.ProcessId != processId) return false;
            AutomationElementCollection elements = root.FindAll(TreeScope.Descendants, Condition.TrueCondition);
            var names = new List<string>();
            var checks = new List<AutomationElement>();
            AutomationElement connect = null, okay = null;
            foreach (AutomationElement element in elements)
            {
                if (element.Current.ProcessId != processId) return false;
                string name = element.Current.Name;
                names.Add(name);
                if (element.Current.ControlType == ControlType.CheckBox) checks.Add(element);
                if (element.Current.ControlType == ControlType.Button)
                {
                    if (name == "Connect" || name == "&Connect") connect = element;
                    if (name == "OK" || name == "&OK") okay = element;
                }
            }
            // These exact English labels come from Microsoft's documented April
            // 2026 RDP-file warnings. A different dialog is a failure, never a
            // generic Yes/Enter click. No certificate or credential UI is accepted.
            // https://learn.microsoft.com/windows-server/remote/remote-desktop-services/remotepc/understanding-security-warnings
            if (root.Current.Name == "Remote Desktop Connection" && names.Contains("Opening Remote Desktop Connection") &&
                checks.Count == 1 && checks[0].Current.Name == EducationalCheck && okay != null)
            {
                object toggleObject;
                if (!checks[0].TryGetCurrentPattern(TogglePattern.Pattern, out toggleObject)) return false;
                var toggle = (TogglePattern)toggleObject;
                if (toggle.Current.ToggleState == ToggleState.Off) toggle.Toggle();
                if (toggle.Current.ToggleState != ToggleState.On || !Invoke(okay)) return false;
                report.Add("client", "progress", "mstsc_rdp_file_education_accepted", null);
                return true;
            }
            bool modern = root.Current.Name == "Remote Desktop Connection security warning" &&
                names.Contains("Caution: Unknown remote connection") && names.Contains("Unknown publisher");
            bool legacy = root.Current.Name == "Remote Desktop Connection" &&
                names.Contains("The publisher of this remote connection can't be identified. Do you want to connect anyway?");
            if ((!modern && !legacy) || !HasEndpoint(names, address) || connect == null) return false;
            foreach (AutomationElement check in checks)
            {
                object pattern;
                if (!check.TryGetCurrentPattern(TogglePattern.Pattern, out pattern) ||
                    ((TogglePattern)pattern).Current.ToggleState != ToggleState.Off) return false;
            }
            if (!Invoke(connect)) return false;
            report.Add("client", "progress", "mstsc_loopback_profile_consent_accepted", new { localFixture = true });
            return true;
        }

        private static bool Invoke(AutomationElement button)
        {
            object pattern;
            if (!button.Current.IsEnabled || !button.TryGetCurrentPattern(InvokePattern.Pattern, out pattern)) return false;
            ((InvokePattern)pattern).Invoke();
            return true;
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

        internal static IntPtr RenderWindow(IntPtr root, uint processId)
        {
            IntPtr result = IntPtr.Zero;
            int best = -1;
            MstscNative.EnumChildWindows(root, delegate(IntPtr window, IntPtr ignored) {
                if (Owner(window) != processId || !MstscNative.IsWindowVisible(window)) return true;
                string name = Class(window);
                int rank = name == "UIOutputClass" ? 3 : name == "UIContainerClass" ? 2 : name == "TscShellAxHostClass" ? 1 : -1;
                Rectangle bounds = ClientBounds(window);
                if (rank > best && bounds.Width >= 640 && bounds.Height >= 480 && Screen.PrimaryScreen.Bounds.Contains(bounds))
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
            return expected != 0 && foreground == expected && focused == expected && sameRoot && focusedClass == "UIInputClass";
        }

        internal static void SendF8(IntPtr root, uint processId)
        {
            Native.RequireInteractiveDesktop();
            MstscNative.SetForegroundWindow(root);
            IntPtr foreground = MstscNative.GetForegroundWindow();
            uint ignored;
            uint thread = MstscNative.GetWindowThreadProcessId(foreground, out ignored);
            var info = new MstscNative.GuiThreadInfo();
            info.Size = Marshal.SizeOf(info);
            if (!MstscNative.GetGUIThreadInfo(thread, ref info) || !FocusAllowed(processId, Owner(foreground), Owner(info.Focus),
                MstscNative.GetAncestor(foreground, 2) == root && MstscNative.GetAncestor(info.Focus, 2) == root, Class(info.Focus)))
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
            IntPtr positioned = IntPtr.Zero;
            double unknownDialogAt = -1;
            int handledDialogs = 0;
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
                        if (state.InitialLayout >= 0 || handledDialogs >= 2 || !MstscDialogs.Handle(window, client.Id, address, report))
                            unknownDialog = true;
                        else handledDialogs++;
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
                int layout = -1, marker = -1;
                if (root != IntPtr.Zero && !unknownDialog)
                {
                    if (positioned != root)
                    {
                        Rectangle area = Screen.PrimaryScreen.WorkingArea;
                        if (!MstscNative.SetWindowPos(root, IntPtr.Zero, area.Left, area.Top,
                            Math.Min(area.Width, 1100), Math.Min(area.Height, 850), 0x0040))
                            throw new SmokeFailure("infrastructure", "mstsc_window_placement_failed");
                        MstscNative.SetForegroundWindow(root);
                        positioned = root;
                    }
                    IntPtr render = MstscWindows.RenderWindow(root, client.Id);
                    if (render != IntPtr.Zero)
                    {
                        using (Bitmap capture = MstscWindows.Capture(render))
                        {
                            Rectangle bounds = MstscPixels.Bounds(capture);
                            if (!bounds.IsEmpty)
                            {
                                using (Bitmap image = MstscPixels.Normalize(capture, bounds))
                                {
                                    layout = Fixture.Match(Fixture.Samples(image));
                                    marker = Fixture.Heartbeat(image);
                                    bool complete = state.Observe(layout, marker, now);
                                    if (!previouslyVisible && state.InitialLayout >= 0)
                                    {
                                        image.Save(Path.Combine(output, "before-input.png"), ImageFormat.Png);
                                        report.Add("render", "passed", "fixture_visible", new { layout = layout,
                                            renderClass = MstscWindows.Class(render), visibleWidth = bounds.Width, visibleHeight = bounds.Height });
                                        MstscWindows.SendF8(root, client.Id);
                                        report.Add("input", "progress", "f8_submitted", new {
                                            method = "SendInput", keyEncoding = "scan_code", controlFocused = true });
                                    }
                                    if (!previouslyConfirmed && state.InputConfirmed)
                                    {
                                        image.Save(Path.Combine(output, "after-input.png"), ImageFormat.Png);
                                        report.Add("input", "passed", "fixture_changed", new { layout = layout });
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
                                }
                            }
                        }
                    }
                }
                if (layout < 0) state.Observe(-1, -1, now);
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
            Check(MstscDialogs.HasEndpoint(new[] { "Remote computer:", "127.0.0.1:3390" }, "127.0.0.1:3390"));
            Check(!MstscDialogs.HasEndpoint(new[] { "127.0.0.1:33900" }, "127.0.0.1:3390"));
            Check(MstscWindows.FocusAllowed(5, 5, 5, true, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 6, 5, true, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 6, true, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 5, false, "UIInputClass"));
            Check(!MstscWindows.FocusAllowed(5, 5, 5, true, "Edit"));
            var state = new MstscObservation();
            state.Observe(0, 0, 0); state.Observe(0, 0, 0.25); state.Observe(-1, -1, 0.5);
            state.Observe(0, 0, 0.75); state.Observe(1, 0, 1); state.Observe(1, 0, 1.25);
            Check(state.InitialLayout < 0);
            state.Observe(1, 0, 1.5);
            Check(state.InitialLayout == 1 && !state.InputConfirmed);
            for (int i = 2; i <= 31; i++) Check(!state.Observe(0, i % 2, i));
            Check(state.InputConfirmed && state.Observe(0, 0, 32));
            var stalled = new MstscObservation();
            stalled.Observe(0, 0, 0); stalled.Observe(0, 0, 0.25); stalled.Observe(0, 0, 0.5);
            Reject(delegate { stalled.Observe(1, 0, 6); });
            var lost = new MstscObservation();
            lost.Observe(0, 0, 0); lost.Observe(0, 0, 0.25); lost.Observe(0, 0, 0.5);
            Reject(delegate { lost.Observe(-1, 1, 4); });
            Reject(delegate { new MstscObservation().Observe(-1, -1, 46); });
            foreach (Size size in new[] { new Size(1024, 768), new Size(960, 720), new Size(800, 600) })
            foreach (int layout in new[] { 0, 1 })
            foreach (int marker in new[] { 0, 1 })
            using (var image = new Bitmap(size.Width + 24, size.Height + 12))
            {
                using (Graphics graphics = Graphics.FromImage(image))
                {
                    graphics.Clear(Color.Black);
                    for (int i = 0; i < 4; i++) using (var brush = new SolidBrush(Fixture.Colors[Fixture.Layouts[layout][i]]))
                        graphics.FillRectangle(brush, 12 + i % 2 * size.Width / 2, 6 + i / 2 * size.Height / 2, size.Width / 2, size.Height / 2);
                    graphics.FillRectangle(marker == 0 ? Brushes.Black : Brushes.Red, 12, 6, size.Width / 64, size.Height / 48);
                }
                Rectangle bounds = MstscPixels.Bounds(image);
                Check(bounds == new Rectangle(12, 6, size.Width, size.Height));
                using (Bitmap normalized = MstscPixels.Normalize(image, bounds))
                { Check(Fixture.Match(Fixture.Samples(normalized)) == layout); Check(Fixture.Heartbeat(normalized) == marker); }
            }
            using (var empty = new Bitmap(1024, 768)) Check(MstscPixels.Bounds(empty).IsEmpty);
            Check(Marshal.SizeOf(typeof(MstscNative.Input)) == (IntPtr.Size == 8 ? 40 : 28));
            Console.WriteLine("mstsc.exe harness self-tests passed");
            return 0;
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
        [DllImport("user32.dll")] internal static extern bool SetWindowPos(IntPtr window, IntPtr after, int x, int y, int width, int height, uint flags);
        [DllImport("user32.dll")] internal static extern bool SetForegroundWindow(IntPtr window);
        [DllImport("user32.dll")] internal static extern IntPtr GetForegroundWindow();
        [DllImport("user32.dll")] internal static extern IntPtr GetAncestor(IntPtr window, uint flags);
        [DllImport("user32.dll")] internal static extern bool GetGUIThreadInfo(uint thread, ref GuiThreadInfo information);
        [DllImport("user32.dll", SetLastError = true)] internal static extern uint SendInput(uint count, Input[] inputs, int size);
    }
}
