// Compile with the inbox .NET Framework 4.x csc.exe; no COM interop package is needed.
// This is a Microsoft mstscax.dll protocol/rendering test, not an mstsc.exe UI test.
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Imaging;
using System.Globalization;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.RegularExpressions;
using System.Web.Script.Serialization;
using System.Windows.Forms;

namespace DevBox.RdpSmoke
{
    internal sealed class SmokeFailure : Exception
    {
        internal readonly string Stage;
        internal readonly string Reason;
        internal SmokeFailure(string stage, string reason) : base(reason) { Stage = stage; Reason = reason; }
    }

    internal sealed class Profile
    {
        internal string Server, UserName, RoutingToken;
        internal int Port, ColorDepth, AuthenticationLevel, AudioMode;
        internal bool CredSsp, NegotiateSecurityLayer, RedirectClipboard, CaptureAudio;

        // Fail closed on unknown/duplicate settings so a changed download cannot be
        // silently tested using different security semantics from the shipped profile.
        internal static Profile Parse(string contents)
        {
            if (contents == null || contents.Length > 32768) throw Invalid();
            var fields = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            var types = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            foreach (string line in contents.Split(new[] { '\r', '\n' }, StringSplitOptions.RemoveEmptyEntries))
            {
                string[] parts = line.Split(new[] { ':' }, 3);
                if (parts.Length != 3 || fields.ContainsKey(parts[0]) || parts[2].IndexOf('\0') >= 0) throw Invalid();
                fields.Add(parts[0], parts[2]);
                types.Add(parts[0], parts[1]);
            }
            string[] strings = { "full address", "username", "loadbalanceinfo" };
            string[] integers = { "screen mode id", "use multimon", "session bpp", "authentication level",
                "enablecredsspsupport", "negotiate security layer", "prompt for credentials",
                "prompt for credentials on client", "autoreconnection enabled", "redirectclipboard",
                "audiomode", "audiocapturemode" };
            if (fields.Count != strings.Length + integers.Length) throw Invalid();
            foreach (string key in strings) if (!types.ContainsKey(key) || types[key] != "s") throw Invalid();
            foreach (string key in integers) if (!types.ContainsKey(key) || types[key] != "i") throw Invalid();
            var profile = new Profile();
            profile.UserName = fields["username"];
            profile.RoutingToken = fields["loadbalanceinfo"];
            if (String.IsNullOrWhiteSpace(profile.UserName) || profile.UserName.Length > 256 ||
                !Regex.IsMatch(profile.RoutingToken, "\\A[0-9a-f]{64}\\z")) throw Invalid();
            ParseAddress(fields["full address"], out profile.Server, out profile.Port);
            profile.ColorDepth = Integer(fields, "session bpp", 16, 24, 32);
            // Authentication level zero explicitly disables server authentication.
            // The smoke test must never turn that into a passing security test.
            profile.AuthenticationLevel = Integer(fields, "authentication level", 1, 2);
            profile.CredSsp = Integer(fields, "enablecredsspsupport", 1) == 1;
            profile.NegotiateSecurityLayer = Integer(fields, "negotiate security layer", 1) == 1;
            profile.RedirectClipboard = Integer(fields, "redirectclipboard", 0, 1) == 1;
            profile.CaptureAudio = Integer(fields, "audiocapturemode", 0, 1) == 1;
            profile.AudioMode = Integer(fields, "audiomode", 0, 1, 2);
            Integer(fields, "screen mode id", 1, 2);
            Integer(fields, "use multimon", 0);
            Integer(fields, "autoreconnection enabled", 0);
            Integer(fields, "prompt for credentials", 0, 1);
            Integer(fields, "prompt for credentials on client", 0, 1);
            return profile;
        }

        private static int Integer(Dictionary<string, string> values, string key, params int[] allowed)
        {
            int value;
            if (!Int32.TryParse(values[key], NumberStyles.None, CultureInfo.InvariantCulture, out value) ||
                Array.IndexOf(allowed, value) < 0) throw Invalid();
            return value;
        }

        private static void ParseAddress(string address, out string server, out int port)
        {
            // Uri handles bracketed IPv6, optional ports, and DNS names without
            // interpreting user-info or a path as part of an RDP server name.
            Uri uri;
            if (!Uri.TryCreate("rdp://" + address, UriKind.Absolute, out uri) ||
                String.IsNullOrEmpty(uri.Host) || uri.UserInfo.Length != 0 || uri.AbsolutePath != "/" ||
                uri.Query.Length != 0 || uri.Fragment.Length != 0 || address.IndexOfAny(new[] { '/', '\\', ' ', '\t' }) >= 0)
                throw Invalid();
            server = uri.Host.Trim('[', ']');
            port = uri.Port == -1 ? 3389 : uri.Port;
            if (port < 1 || port > 65535) throw Invalid();
        }

        internal string PackedRoutingToken()
        {
            // LoadBalanceInfo transmits the *bytes* of a BSTR, not UTF-8 text.
            // The gateway accepts exactly 64 hex characters followed by CRLF.
            // Do not add a Cookie prefix or pad/change a one-time routing grant.
            byte[] bytes = Encoding.ASCII.GetBytes(RoutingToken + "\r\n");
            return Encoding.Unicode.GetString(bytes);
        }

        private static SmokeFailure Invalid() { return new SmokeFailure("profile", "unsupported_or_invalid_profile"); }

        internal void RequireLoopback()
        {
            if (Server != "127.0.0.1" || UserName != "rdp-smoke")
                throw new SmokeFailure("configuration", "local_fixture_must_use_loopback");
        }
    }

    internal static class Fixture
    {
        internal static readonly Color[] Colors = { Color.FromArgb(220, 40, 40), Color.FromArgb(40, 200, 60),
            Color.FromArgb(40, 80, 220), Color.FromArgb(220, 190, 40) };
        internal static readonly int[][] Layouts = { new[] { 0, 1, 2, 3 }, new[] { 2, 3, 0, 1 } };

        internal static Color[] Samples(Bitmap bitmap)
        {
            var samples = new Color[4];
            for (int i = 0; i < 4; i++)
            {
                int x = bitmap.Width * (i % 2 == 0 ? 1 : 3) / 4;
                int y = bitmap.Height * (i < 2 ? 1 : 3) / 4;
                int r = 0, g = 0, b = 0;
                // Average a small patch to tolerate chroma subsampling and avoid
                // accepting a single coincidentally matching pixel or cursor.
                for (int dy = -3; dy <= 3; dy++) for (int dx = -3; dx <= 3; dx++)
                {
                    Color c = bitmap.GetPixel(x + dx, y + dy);
                    r += c.R; g += c.G; b += c.B;
                }
                samples[i] = Color.FromArgb(r / 49, g / 49, b / 49);
            }
            return samples;
        }

        internal static int Match(Color[] samples)
        {
            if (samples == null || samples.Length != 4) return -1;
            for (int layout = 0; layout < Layouts.Length; layout++)
            {
                bool match = true;
                for (int i = 0; i < 4; i++) match &= Near(samples[i], Colors[Layouts[layout][i]], 40);
                if (match) return layout;
            }
            return -1;
        }

        internal static bool Near(Color actual, Color expected, int tolerance)
        {
            return Math.Abs(actual.R - expected.R) <= tolerance && Math.Abs(actual.G - expected.G) <= tolerance &&
                Math.Abs(actual.B - expected.B) <= tolerance;
        }

        internal static int Heartbeat(Bitmap image)
        {
            // The local server alternates a 16x16 corner patch every frame.
            // Sample its interior, away from edges affected by H.264 filtering.
            int r = 0, g = 0, b = 0;
            for (int y = 5; y < 12; y++) for (int x = 5; x < 12; x++)
            {
                Color c = image.GetPixel(x, y);
                r += c.R; g += c.G; b += c.B;
            }
            Color sample = Color.FromArgb(r / 49, g / 49, b / 49);
            if (Near(sample, Color.Black, 40)) return 0;
            if (Near(sample, Color.Red, 40)) return 1;
            return -1;
        }
    }

    internal sealed class HeartbeatMonitor
    {
        private int previous = -1;
        internal int Changes;
        internal double LastChangeAt;

        internal void Observe(int marker, double now)
        {
            if (marker < 0 || marker == previous) return;
            if (previous >= 0) Changes++;
            previous = marker;
            LastChangeAt = now;
        }

        internal bool Stalled(double now, double firstFrameAt)
        {
            return now - Math.Max(firstFrameAt, LastChangeAt) > 5;
        }

        internal bool Verified(double now)
        {
            return Changes >= 4 && now - LastChangeAt <= 5;
        }
    }

    internal sealed class Report
    {
        private readonly string path;
        private readonly object gate = new object();
        private readonly List<object> events = new List<object>();
        internal Report(string output) { path = Path.Combine(output, "client-events.json"); }
        internal void Add(string stage, string outcome, string reason, object details)
        {
            lock (gate)
            {
                events.Add(new { timestamp = DateTime.UtcNow.ToString("o"), stage = stage,
                    outcome = outcome, reason = reason, details = details });
                File.WriteAllText(path, new JavaScriptSerializer().Serialize(new { version = 1, events = events }));
            }
            // Only fixed application labels are sent to the console. Never print
            // exception.Message, server/profile contents, credentials, or COM text.
            Console.WriteLine(stage + ": " + outcome + (reason == null ? "" : " (" + reason + ")"));
        }
    }

    internal sealed class RdpControlClass
    {
        internal readonly string Name;
        internal readonly Guid Clsid;
        internal RdpControlClass(string name, string clsid) { Name = name; Clsid = new Guid(clsid); }
    }

    internal static class RdpControls
    {
        internal const int ClassNotRegistered = unchecked((int)0x80040154);
        // These are coclass IDs, not ProgIDs. The coclass names alone do not
        // resolve through Type.GetTypeFromProgID on the hosted Windows images.
        // https://learn.microsoft.com/windows/win32/termserv/imsrdpclient10
        // https://learn.microsoft.com/windows/win32/termserv/imsrdpclient8
        private static readonly RdpControlClass[] Classes = {
            new RdpControlClass("MsRdpClient10NotSafeForScripting", "A0C63C30-F08D-4AB4-907C-34905D770C7D"),
            new RdpControlClass("MsRdpClient9NotSafeForScripting", "8B918B82-7985-4C24-89DF-C33AD2BBFBCD"),
            new RdpControlClass("MsRdpClient8NotSafeForScripting", "A3BC03A0-041D-42E3-AD22-882B7865C9C5")
        };

        internal static T Activate<T>(Func<RdpControlClass, T> activate, Report report)
        {
            foreach (RdpControlClass candidate in Classes)
            {
                if (report != null) report.Add("infrastructure", "progress", "microsoft_rdp_control_activation",
                    new { controlClass = candidate.Name, clsid = candidate.Clsid.ToString() });
                T result;
                try { result = activate(candidate); }
                catch (Exception error)
                {
                    int hresult = error.GetBaseException().HResult;
                    if (report != null) report.Add("infrastructure", "progress", "microsoft_rdp_control_activation_error",
                        new { controlClass = candidate.Name, clsid = candidate.Clsid.ToString(), hresult = hresult });
                    // Only a missing class permits fallback. Do not conceal a
                    // broken installed control by trying an older version.
                    if (hresult != ClassNotRegistered) throw;
                    continue;
                }
                if (report != null) report.Add("infrastructure", "passed", "microsoft_rdp_control_activated",
                    new { controlClass = candidate.Name, clsid = candidate.Clsid.ToString() });
                return result;
            }
            throw new SmokeFailure("infrastructure", "microsoft_rdp_control_unavailable");
        }
    }

    internal sealed class RdpHost : AxHost
    {
        internal readonly RdpControlClass ControlClass;
        private RdpHost(RdpControlClass controlClass) : base(controlClass.Clsid.ToString()) { ControlClass = controlClass; }
        internal object Client { get { return GetOcx(); } }

        internal static RdpHost Attach(Control parent, Report report)
        {
            return RdpControls.Activate(delegate(RdpControlClass candidate) {
                var host = new RdpHost(candidate);
                try
                {
                    ((ISupportInitialize)host).BeginInit();
                    host.Dock = DockStyle.Fill;
                    parent.Controls.Add(host);
                    ((ISupportInitialize)host).EndInit();
                    host.CreateControl();
                    // Getting a Type from a CLSID does not prove activation.
                    // Require the actual hosted COM object before accepting it.
                    if (host.Client == null) throw new SmokeFailure("infrastructure", "microsoft_rdp_control_not_created");
                    return host;
                }
                catch
                {
                    parent.Controls.Remove(host);
                    host.Dispose();
                    throw;
                }
            }, report);
        }
    }

    internal sealed class SmokeWindow : Form
    {
        private readonly Profile profile;
        private string password;
        private readonly string output;
        private readonly int duration;
        private readonly bool localFixture;
        private readonly Report report;
        private readonly HeartbeatMonitor heartbeat = new HeartbeatMonitor();
        private readonly Stopwatch clock = Stopwatch.StartNew();
        private readonly Timer poll = new Timer();
        private readonly List<KeyValuePair<int, Delegate>> subscriptions = new List<KeyValuePair<int, Delegate>>();
        private static readonly Guid EventsIID = new Guid("336d5562-efa8-482e-8cb3-c5c0fc7a7db6");
        private RdpHost host;
        private object ocx;
        private double connectAt, firstFrameAt, keyAt, lastGoodAt;
        private int initialLayout = -1, stableSamples;
        private bool started, inputConfirmed, finishing;
        internal int Result = 1;

        internal SmokeWindow(Profile profile, string password, string output, int duration, Report report, bool localFixture)
        {
            this.profile = profile; this.password = password; this.output = output;
            this.duration = duration; this.report = report;
            this.localFixture = localFixture;
            AutoScaleMode = AutoScaleMode.None;
            ClientSize = new Size(1024, 768);
            FormBorderStyle = FormBorderStyle.None;
            StartPosition = FormStartPosition.Manual;
            Location = Screen.PrimaryScreen.Bounds.Location;
            TopMost = true;
            Text = "DevBox RDP compatibility smoke test";
            BackColor = Color.Magenta;
            Paint += delegate(object sender, PaintEventArgs e) {
                if (!started) e.Graphics.FillRectangle(Brushes.Cyan, 0, 0, ClientSize.Width / 2, ClientSize.Height);
            };
            poll.Interval = 250;
            poll.Tick += Poll;
            Shown += delegate { Activate(); poll.Start(); };
            FormClosing += delegate { if (!finishing) Fail("client", "window_closed", null); };
        }

        private void StartClient()
        {
            Native.RequireInteractiveDesktop();
            if (!Screen.PrimaryScreen.Bounds.Contains(RectangleToScreen(ClientRectangle)))
                throw new SmokeFailure("infrastructure", "desktop_too_small");
            using (Bitmap image = CaptureContent())
            {
                Color[] samples = Fixture.Samples(image);
                if (!Fixture.Near(samples[0], Color.Cyan, 5) || !Fixture.Near(samples[1], Color.Magenta, 5) ||
                    !Fixture.Near(samples[2], Color.Cyan, 5) || !Fixture.Near(samples[3], Color.Magenta, 5))
                    throw new SmokeFailure("infrastructure", "screen_capture_probe_failed");
            }
            report.Add("infrastructure", "passed", null, new { desktopWidth = 1024, desktopHeight = 768 });
            host = RdpHost.Attach(this, report);
            started = true;
            ocx = host.Client;
            Subscribe(2, new Action(delegate { report.Add("connection", "progress", "connected_event", null); }));
            Subscribe(3, new Action(delegate { report.Add("connection", "progress", "login_complete_event", null); }));
            Subscribe(4, new Action<int>(Disconnected));
            Subscribe(10, new Action<int>(delegate(int code) { Fail("client", "fatal_error", new { code = code }); }));
            Subscribe(11, new Action<int>(delegate(int code) { report.Add("client", "warning", "client_warning", new { code = code }); }));
            Subscribe(18, new Action(delegate { Fail("authentication", "interactive_authentication_warning", null); }));
            dynamic client = ocx;
            dynamic advanced = client.AdvancedSettings8;
            client.Server = profile.Server;
            client.UserName = profile.UserName;
            client.DesktopWidth = 1024;
            client.DesktopHeight = 768;
            client.ColorDepth = profile.ColorDepth;
            client.FullScreen = false;
            client.ConnectingText = "Connecting to the isolated test desktop";
            client.DisconnectedText = "Test session disconnected";
            advanced.RDPPort = profile.Port;
            advanced.AuthenticationLevel = (uint)profile.AuthenticationLevel;
            advanced.EnableCredSspSupport = profile.CredSsp;
            // A direct rdp.Accept fixture is not a gateway and does not consume
            // the gateway's bare routing token. Leave the default client cookie
            // intact for localhost; production gateway testing is unchanged.
            if (!localFixture) advanced.LoadBalanceInfo = profile.PackedRoutingToken();
            advanced.EnableAutoReconnect = false;
            advanced.RedirectClipboard = profile.RedirectClipboard;
            advanced.AudioRedirectionMode = (uint)profile.AudioMode;
            advanced.AudioCaptureRedirectionMode = profile.CaptureAudio;
            advanced.SmartSizing = false;
            advanced.overallConnectionTimeout = 45;
            advanced.singleConnectionTimeout = 45;
            // No local drive/printer/smart-card data is required by the fixture.
            advanced.RedirectDrives = false;
            advanced.RedirectPrinters = false;
            advanced.RedirectPorts = false;
            advanced.RedirectSmartCards = false;
            var nonscript = (INonScriptable3)ocx;
            nonscript.SetUIParentWindowHandle(Handle);
            nonscript.SetNegotiateSecurityLayer(profile.NegotiateSecurityLayer);
            // Explicit automation override: test password transmission, not the
            // human credential-dialog UX. Certificate verification stays enabled.
            nonscript.SetPromptForCredentials(false);
            nonscript.SetClearTextPassword(password);
            password = null;
            report.Add("client", "configured", null, new { controlClass = host.ControlClass.Name,
                clsid = host.ControlClass.Clsid.ToString(),
                colorDepth = profile.ColorDepth,
                authenticationLevel = profile.AuthenticationLevel, credSsp = profile.CredSsp,
                audioMode = profile.AudioMode, credentialPromptOverride = true, localFixture = localFixture });
            connectAt = clock.Elapsed.TotalSeconds;
            client.Connect();
        }

        private void Subscribe(int id, Delegate handler)
        {
            ComEventsHelper.Combine(ocx, EventsIID, id, handler);
            subscriptions.Add(new KeyValuePair<int, Delegate>(id, handler));
        }

        private void Poll(object sender, EventArgs args)
        {
            if (finishing) return;
            try
            {
                if (!started)
                {
                    if (clock.Elapsed.TotalSeconds >= 1) StartClient();
                    return;
                }
                Native.RequireInteractiveDesktop();
                double now = clock.Elapsed.TotalSeconds;
                dynamic client = ocx;
                int connected = (int)client.Connected;
                using (Bitmap image = CaptureContent())
                {
                    int layout = Fixture.Match(Fixture.Samples(image));
                    if (initialLayout < 0)
                    {
                        if (layout < 0 || connected != 1) { stableSamples = 0; }
                        else if (++stableSamples >= 3)
                        {
                            initialLayout = layout;
                            firstFrameAt = lastGoodAt = now;
                            Save(image, "before-input.png", false);
                            report.Add("render", "passed", "fixture_visible", new { layout = layout });
                            // WM_KEYDOWN lParam: repeat=1, scan code=0x42 (F8).
                            // Key-up also sets previous-state and transition bits.
                            var keys = (INonScriptable3)ocx;
                            keys.SendKeys(2, new short[] { 0, -1 }, new[] { 0x00420001, unchecked((int)0xC0420001) });
                            keyAt = now;
                            report.Add("input", "progress", "f8_sent", null);
                        }
                        if (initialLayout < 0 && now - connectAt > 45)
                            Fail("render", "fixture_not_visible", null);
                        return;
                    }
                    if (connected != 1) { Fail("connection", "connection_state_lost", null); return; }
                    if (localFixture)
                    {
                        heartbeat.Observe(Fixture.Heartbeat(image), now);
                        if (heartbeat.Stalled(now, firstFrameAt))
                        { Fail("render", "heartbeat_stalled", null); return; }
                    }
                    if (!inputConfirmed && layout == 1 - initialLayout)
                    {
                        inputConfirmed = true;
                        Save(image, "after-input.png", false);
                        report.Add("input", "passed", "fixture_changed", new { layout = layout });
                    }
                    if (!inputConfirmed && now - keyAt > 10) { Fail("input", "fixture_did_not_change", null); return; }
                    if (layout == (inputConfirmed ? 1 - initialLayout : initialLayout)) lastGoodAt = now;
                    if (now - lastGoodAt > 3) { Fail("render", "fixture_lost", null); return; }
                    if (inputConfirmed && layout == 1 - initialLayout && now - firstFrameAt >= duration)
                    {
                        if (localFixture)
                        {
                            if (!heartbeat.Verified(now)) { Fail("render", "heartbeat_not_verified", null); return; }
                            report.Add("render", "passed", "heartbeat_verified", new {
                                changes = heartbeat.Changes, lastChangeAgeSeconds = now - heartbeat.LastChangeAt });
                        }
                        Save(image, "terminal.png", false);
                        report.Add("session", "passed", "render_input_and_duration_verified",
                            new { visibleSeconds = now - firstFrameAt, requiredSeconds = duration });
                        Finish(0);
                    }
                }
            }
            catch (SmokeFailure error) { Fail(error.Stage, error.Reason, null); }
            catch (Exception error) { Fail("client", "operation_failed", new { hresult = error.HResult }); }
        }

        private Bitmap CaptureContent()
        {
            var image = new Bitmap(ClientSize.Width, ClientSize.Height, PixelFormat.Format24bppRgb);
            try
            {
                using (Graphics graphics = Graphics.FromImage(image))
                    graphics.CopyFromScreen(PointToScreen(Point.Empty), Point.Empty, image.Size, CopyPixelOperation.SourceCopy);
                return image;
            }
            catch { image.Dispose(); throw new SmokeFailure("infrastructure", "screen_capture_unavailable"); }
        }

        private void Save(Bitmap image, string name, bool redact)
        {
            if (!redact) image.Save(Path.Combine(output, name), ImageFormat.Png);
            else
            {
                // Unknown screens can contain account names or certificate text.
                // Retain only averaged quadrant colours, never their raw pixels.
                Color[] samples = Fixture.Samples(image);
                using (var redacted = new Bitmap(320, 240))
                using (Graphics graphics = Graphics.FromImage(redacted))
                {
                    for (int i = 0; i < samples.Length; i++)
                        using (var brush = new SolidBrush(samples[i]))
                            graphics.FillRectangle(brush, (i % 2) * 160, (i / 2) * 120, 160, 120);
                    redacted.Save(Path.Combine(output, name), ImageFormat.Png);
                }
                report.Add("artifact", "recorded", "terminal_image_redacted_to_sample_colours", null);
            }
        }

        private void Disconnected(int reason)
        {
            int? extended = null;
            try { dynamic client = ocx; extended = (int)client.ExtendedDisconnectReason; } catch { }
            report.Add("connection", finishing ? "closed" : "failed", finishing ? "intentional_disconnect" : "unexpected_disconnect",
                new { disconnectReason = reason, extendedDisconnectReason = extended });
            if (!finishing) Fail("connection", "unexpected_disconnect", null);
        }

        internal void Fail(string stage, string reason, object details)
        {
            if (finishing) return;
            report.Add(stage, "failed", reason, details);
            try
            {
                using (Bitmap image = CaptureContent()) Save(image, "terminal.png", Fixture.Match(Fixture.Samples(image)) < 0);
            }
            catch { report.Add("artifact", "unavailable", "terminal_capture_failed", null); }
            Finish(1);
        }

        private void Finish(int result)
        {
            if (finishing) return;
            finishing = true;
            Result = result;
            poll.Stop();
            // Dispatch after returning from a COM callback; never tear down the
            // ActiveX object while it is raising an event into this application.
            BeginInvoke(new Action(delegate {
                try { if (ocx != null) { dynamic client = ocx; client.Disconnect(); } } catch { }
                Close();
            }));
        }

        protected override void Dispose(bool disposing)
        {
            if (disposing)
            {
                poll.Dispose();
                foreach (KeyValuePair<int, Delegate> entry in subscriptions)
                    try { ComEventsHelper.Remove(ocx, EventsIID, entry.Key, entry.Value); } catch { }
            }
            base.Dispose(disposing);
        }
    }

    // Flattened IMsTscNonScriptable -> IMsRdpClientNonScriptable -> ...2 -> ...3
    // vtable prefix. Do not reorder these methods or remove the unused slots.
    // SendKeys is NOT IDispatch: dynamic invocation cannot call it correctly.
    // https://learn.microsoft.com/windows/win32/termserv/imsrdpclientnonscriptable-interface
    [ComImport, Guid("b3378d90-0728-45c7-8ed7-b6159fb92219"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface INonScriptable3
    {
        void SetClearTextPassword([MarshalAs(UnmanagedType.BStr)] string password);
        void SetPortablePassword([MarshalAs(UnmanagedType.BStr)] string value);
        void GetPortablePassword([MarshalAs(UnmanagedType.BStr)] out string value);
        void SetPortableSalt([MarshalAs(UnmanagedType.BStr)] string value);
        void GetPortableSalt([MarshalAs(UnmanagedType.BStr)] out string value);
        void SetBinaryPassword([MarshalAs(UnmanagedType.BStr)] string value);
        void GetBinaryPassword([MarshalAs(UnmanagedType.BStr)] out string value);
        void SetBinarySalt([MarshalAs(UnmanagedType.BStr)] string value);
        void GetBinarySalt([MarshalAs(UnmanagedType.BStr)] out string value);
        void ResetPassword();
        void NotifyRedirectDeviceChange(UIntPtr wParam, IntPtr lParam);
        void SendKeys(int count, [In, MarshalAs(UnmanagedType.LPArray, SizeParamIndex = 0)] short[] keyUp,
            [In, MarshalAs(UnmanagedType.LPArray, SizeParamIndex = 0)] int[] keyData);
        void SetUIParentWindowHandle(IntPtr handle);
        void GetUIParentWindowHandle(out IntPtr handle);
        void SetShowRedirectionWarningDialog([MarshalAs(UnmanagedType.VariantBool)] bool value);
        void GetShowRedirectionWarningDialog([MarshalAs(UnmanagedType.VariantBool)] out bool value);
        void SetPromptForCredentials([MarshalAs(UnmanagedType.VariantBool)] bool value);
        void GetPromptForCredentials([MarshalAs(UnmanagedType.VariantBool)] out bool value);
        void SetNegotiateSecurityLayer([MarshalAs(UnmanagedType.VariantBool)] bool value);
        void GetNegotiateSecurityLayer([MarshalAs(UnmanagedType.VariantBool)] out bool value);
    }

    internal static class Native
    {
        [DllImport("user32.dll")] internal static extern bool SetProcessDPIAware();
        [DllImport("user32.dll", SetLastError = true)] private static extern IntPtr OpenInputDesktop(uint flags, bool inherit, uint access);
        [DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern bool GetUserObjectInformation(IntPtr handle, int index, StringBuilder value, int length, out int needed);
        [DllImport("user32.dll")] private static extern bool CloseDesktop(IntPtr desktop);

        internal static object RuntimeMetadata()
        {
            // Environment.OSVersion can report Windows 8 for a Framework app
            // without a supportedOS manifest. Read only fixed OS identity values.
            string product = "unavailable", build = "unavailable", revision = "unavailable", dllVersion = "unavailable";
            try
            {
                using (Microsoft.Win32.RegistryKey key = Microsoft.Win32.Registry.LocalMachine.OpenSubKey(
                    @"SOFTWARE\Microsoft\Windows NT\CurrentVersion"))
                {
                    if (key != null)
                    {
                        product = Convert.ToString(key.GetValue("ProductName"), CultureInfo.InvariantCulture);
                        build = Convert.ToString(key.GetValue("CurrentBuildNumber"), CultureInfo.InvariantCulture);
                        revision = Convert.ToString(key.GetValue("UBR"), CultureInfo.InvariantCulture);
                    }
                }
            }
            catch { }
            try { dllVersion = FileVersionInfo.GetVersionInfo(Path.Combine(Environment.SystemDirectory, "mstscax.dll")).FileVersion; }
            catch { }
            return new { osProduct = product, osBuild = build, osUpdateRevision = revision,
                mstscaxVersion = dllVersion, process64Bit = Environment.Is64BitProcess,
                interactive = Environment.UserInteractive, sessionId = Process.GetCurrentProcess().SessionId };
        }

        internal static void RequireInteractiveDesktop()
        {
            if (!Environment.UserInteractive || Process.GetCurrentProcess().SessionId == 0)
                throw new SmokeFailure("infrastructure", "interactive_desktop_required");
            IntPtr desktop = OpenInputDesktop(0, false, 1);
            if (desktop == IntPtr.Zero) throw new SmokeFailure("infrastructure", "input_desktop_unavailable");
            try
            {
                var name = new StringBuilder(256);
                int needed;
                if (!GetUserObjectInformation(desktop, 2, name, name.Capacity * 2, out needed) ||
                    !String.Equals(name.ToString(), "Default", StringComparison.OrdinalIgnoreCase))
                    throw new SmokeFailure("infrastructure", "desktop_locked_or_unavailable");
            }
            finally { CloseDesktop(desktop); }
        }
    }

    internal static class Program
    {
        [STAThread]
        private static int Main(string[] args)
        {
            Report report = null;
            try
            {
                if (args.Length == 1 && args[0] == "--self-test") return SelfTest();
                if (args.Length == 1 && args[0] == "--check-control") return CheckControl();
                if (args.Length != 0) throw new SmokeFailure("configuration", "unexpected_arguments");
                string output = Environment.GetEnvironmentVariable("RDP_SMOKE_OUTPUT");
                if (String.IsNullOrWhiteSpace(output)) throw new SmokeFailure("configuration", "output_directory_required");
                Directory.CreateDirectory(output);
                report = new Report(output);
                report.Add("client", "started", null, new { harness = "Microsoft RDP ActiveX", schema = 1 });
                report.Add("environment", "recorded", null, Native.RuntimeMetadata());
                string profilePath = Environment.GetEnvironmentVariable("RDP_SMOKE_PROFILE");
                string password = Environment.GetEnvironmentVariable("RDP_SMOKE_PASSWORD");
                Environment.SetEnvironmentVariable("RDP_SMOKE_PASSWORD", null);
                Environment.SetEnvironmentVariable("RDP_TEST_PASSWORD", null);
                if (String.IsNullOrWhiteSpace(profilePath) || String.IsNullOrEmpty(password))
                    throw new SmokeFailure("configuration", "profile_and_password_required");
                int duration = 30;
                string durationText = Environment.GetEnvironmentVariable("RDP_SMOKE_DURATION_SECONDS");
                if (durationText != null && (!Int32.TryParse(durationText, out duration) || duration < 30 || duration > 120))
                    throw new SmokeFailure("configuration", "duration_out_of_range");
                Profile profile = Profile.Parse(File.ReadAllText(profilePath));
                string localMode = Environment.GetEnvironmentVariable("RDP_SMOKE_LOCAL");
                if (localMode != null && localMode != "0" && localMode != "1")
                    throw new SmokeFailure("configuration", "invalid_local_fixture_mode");
                bool localFixture = localMode == "1";
                if (localFixture) profile.RequireLoopback();
                Native.RequireInteractiveDesktop();
                Native.SetProcessDPIAware();
                Application.EnableVisualStyles();
                Application.SetCompatibleTextRenderingDefault(false);
                Application.SetUnhandledExceptionMode(UnhandledExceptionMode.ThrowException);
                // A blocked COM/modal call must not wait forever, even if its STA
                // no longer pumps the WinForms timer. The parent also has a timeout.
                using (var watchdog = new System.Threading.Timer(delegate {
                    try { report.Add("infrastructure", "failed", "client_watchdog_timeout", null); } catch { }
                    Environment.Exit(2);
                }, null, (duration + 90) * 1000, System.Threading.Timeout.Infinite))
                using (var window = new SmokeWindow(profile, password, output, duration, report, localFixture))
                {
                    password = null;
                    Application.Run(window);
                    return window.Result;
                }
            }
            catch (SmokeFailure error)
            {
                if (report != null) report.Add(error.Stage, "failed", error.Reason, null);
                else Console.WriteLine("configuration: failed");
                return 1;
            }
            catch (Exception error)
            {
                if (report != null) report.Add("client", "failed", "operation_failed", new { hresult = error.HResult });
                else Console.WriteLine("client: failed");
                return 1;
            }
        }

        private static int SelfTest()
        {
            ControlSelectionTest();
            string token = new string('a', 64);
            string valid = "full address:s:example.invalid:3390\r\nusername:s:test-user\r\nloadbalanceinfo:s:" + token + "\r\n" +
                "screen mode id:i:2\r\nuse multimon:i:0\r\nsession bpp:i:32\r\nauthentication level:i:2\r\n" +
                "enablecredsspsupport:i:1\r\nnegotiate security layer:i:1\r\nprompt for credentials:i:1\r\n" +
                "prompt for credentials on client:i:1\r\nautoreconnection enabled:i:0\r\nredirectclipboard:i:1\r\n" +
                "audiomode:i:0\r\naudiocapturemode:i:0\r\n";
            Profile profile = Profile.Parse(valid);
            Check(profile.Server == "example.invalid" && profile.Port == 3390 && profile.ColorDepth == 32);
            Check(Encoding.ASCII.GetString(Encoding.Unicode.GetBytes(profile.PackedRoutingToken())) == token + "\r\n");
            Check(Profile.Parse(valid.Replace("example.invalid:3390", "[2001:db8::1]:3389")).Port == 3389);
            Check(Profile.Parse(valid.Replace("example.invalid:3390", "example.invalid")).Port == 3389);
            Profile.Parse(valid.Replace("example.invalid:3390", "127.0.0.1:3390").Replace("test-user", "rdp-smoke")).RequireLoopback();
            bool nonlocalRejected = false;
            try { profile.RequireLoopback(); } catch (SmokeFailure) { nonlocalRejected = true; }
            Check(nonlocalRejected);
            foreach (string bad in new[] { valid + "username:s:duplicate\r\n", valid.Replace("session bpp:i:32", "session bpp:s:32"),
                valid.Replace("authentication level:i:2", "authentication level:i:0"), valid.Replace("enablecredsspsupport:i:1", "enablecredsspsupport:i:0"),
                valid.Replace(token, token + " "), valid.Replace("example.invalid:3390", "user@example.invalid"),
                valid.Replace("example.invalid:3390", "example.invalid/path"), valid + "unknown:i:1\r\n" })
            {
                bool rejected = false;
                try { Profile.Parse(bad); } catch (SmokeFailure) { rejected = true; }
                Check(rejected);
            }
            foreach (int layout in new[] { 0, 1 })
            {
                using (var image = new Bitmap(1024, 768))
                using (Graphics graphics = Graphics.FromImage(image))
                {
                    for (int i = 0; i < 4; i++)
                        using (var brush = new SolidBrush(Fixture.Colors[Fixture.Layouts[layout][i]]))
                            graphics.FillRectangle(brush, (i % 2) * 512, (i / 2) * 384, 512, 384);
                    Check(Fixture.Match(Fixture.Samples(image)) == layout);
                    graphics.FillRectangle(Brushes.Black, 0, 0, 16, 16);
                    Check(Fixture.Heartbeat(image) == 0);
                    graphics.FillRectangle(Brushes.Red, 0, 0, 16, 16);
                    Check(Fixture.Heartbeat(image) == 1);
                    graphics.FillRectangle(Brushes.Lime, 0, 0, 16, 16);
                    Check(Fixture.Heartbeat(image) == -1);
                }
            }
            var heartbeat = new HeartbeatMonitor();
            Check(!heartbeat.Verified(0) && !heartbeat.Stalled(1, 0));
            heartbeat.Observe(-1, 1);
            Check(heartbeat.Stalled(6, 0));
            for (int i = 0; i < 5; i++) heartbeat.Observe(i % 2, i + 1);
            Check(heartbeat.Changes == 4 && heartbeat.Verified(6));
            heartbeat.Observe(0, 11); // A frozen valid image must not refresh liveness.
            Check(!heartbeat.Verified(11) && heartbeat.Stalled(11, 0));
            heartbeat.Observe(-1, 12);
            Check(heartbeat.Changes == 4 && heartbeat.Stalled(12, 0));
            Check(Fixture.Match(new[] { Color.Black, Color.Black, Color.Black, Color.Black }) == -1);
            Check(Fixture.Match(new[] { Fixture.Colors[0], Fixture.Colors[0], Fixture.Colors[0], Fixture.Colors[0] }) == -1);
            Console.WriteLine("client self-tests: passed");
            return 0;
        }

        private static int CheckControl()
        {
            // Exercise the same AxHost path as the live test without connecting,
            // accepting credentials, or changing trust. Parser tests cannot tell
            // whether this Windows image can create Microsoft's actual control.
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.SetUnhandledExceptionMode(UnhandledExceptionMode.ThrowException);
            Exception failure = null;
            bool activated = false;
            using (var watchdog = new System.Threading.Timer(delegate {
                Console.WriteLine("Microsoft RDP control preflight: failed (activation_timeout)");
                Environment.Exit(2);
            }, null, 30000, System.Threading.Timeout.Infinite))
            using (var window = new Form())
            {
                window.Text = "DevBox Microsoft RDP component check";
                window.ClientSize = new Size(480, 320);
                window.Shown += delegate {
                    try
                    {
                        RdpHost host = RdpHost.Attach(window, null);
                        activated = true;
                        Console.WriteLine("Microsoft RDP control preflight: passed (" + host.ControlClass.Name + ")");
                    }
                    catch (Exception error) { failure = error; }
                    finally { window.BeginInvoke(new Action(window.Close)); }
                };
                Application.Run(window);
            }
            if (failure == null && activated) return 0;
            if (failure == null) failure = new SmokeFailure("infrastructure", "microsoft_rdp_control_not_created");
            var known = failure as SmokeFailure;
            Console.WriteLine("Microsoft RDP control preflight: failed (" +
                (known == null ? "activation_failed" : known.Reason) + ", HRESULT 0x" +
                failure.GetBaseException().HResult.ToString("X8", CultureInfo.InvariantCulture) + ")");
            return 1;
        }

        private static void ControlSelectionTest()
        {
            Guid[] expected = { new Guid("A0C63C30-F08D-4AB4-907C-34905D770C7D"),
                new Guid("8B918B82-7985-4C24-89DF-C33AD2BBFBCD"), new Guid("A3BC03A0-041D-42E3-AD22-882B7865C9C5") };
            // A fake activator exercises all availability combinations without
            // requiring COM; --check-control separately tests actual activation.
            for (int available = 0; available < expected.Length; available++)
            {
                int attempts = 0;
                Guid selected = RdpControls.Activate(delegate(RdpControlClass candidate) {
                    int index = attempts++;
                    Check(candidate.Clsid == expected[index]);
                    if (index != available) throw new COMException("unregistered", RdpControls.ClassNotRegistered);
                    return candidate.Clsid;
                }, null);
                Check(selected == expected[available] && attempts == available + 1);
            }
            int missingAttempts = 0;
            bool unavailable = false;
            try
            {
                RdpControls.Activate<int>(delegate(RdpControlClass candidate) {
                    Check(candidate.Clsid == expected[missingAttempts++]);
                    throw new COMException("unregistered", RdpControls.ClassNotRegistered);
                }, null);
            }
            catch (SmokeFailure error) { unavailable = error.Reason == "microsoft_rdp_control_unavailable"; }
            Check(unavailable && missingAttempts == expected.Length);
            int failedAttempts = 0;
            bool propagated = false;
            try
            {
                RdpControls.Activate<int>(delegate(RdpControlClass candidate) {
                    failedAttempts++;
                    throw new COMException("failed", unchecked((int)0x80004005));
                }, null);
            }
            catch (COMException error) { propagated = error.HResult == unchecked((int)0x80004005); }
            Check(propagated && failedAttempts == 1);
        }

        private static void Check(bool condition)
        {
            if (!condition) throw new SmokeFailure("self_test", "assertion_failed");
        }
    }
}
