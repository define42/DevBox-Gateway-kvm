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

    internal struct PerformanceMarker
    {
        internal int Sequence;
        internal byte InputEpoch;

        internal static bool TryRead(Bitmap image, out PerformanceMarker marker)
        {
            marker = new PerformanceMarker();
            if (image.Width < 640 || image.Height < 360) return false;
            ulong bits = 0;
            for (int bit = 0; bit < 40; bit++)
            {
                int x = (64 + bit * 32 + 16) * image.Width / 1920;
                int normal = Sample(image, x, 32 * image.Height / 1080);
                int inverse = Sample(image, x, 72 * image.Height / 1080);
                if (normal < 0 || inverse < 0 || normal == inverse) return false;
                bits |= (ulong)normal << bit;
            }
            int sequence = (int)(bits & 0xffffff);
            byte epoch = (byte)(bits >> 24);
            byte checksum = (byte)(sequence ^ (sequence >> 8) ^ (sequence >> 16) ^ epoch ^ 0xa5);
            if ((byte)(bits >> 32) != checksum) return false;
            marker.Sequence = sequence;
            marker.InputEpoch = epoch;
            return true;
        }

        private static int Sample(Bitmap image, int x, int y)
        {
            int sum = 0;
            for (int dy = -1; dy <= 1; dy++) for (int dx = -1; dx <= 1; dx++)
            {
                Color pixel = image.GetPixel(x + dx, y + dy);
                if (Math.Abs(pixel.R - pixel.G) > 40 || Math.Abs(pixel.R - pixel.B) > 40) return -1;
                sum += pixel.R + pixel.G + pixel.B;
            }
            int mean = sum / 27;
            return mean <= 64 ? 0 : mean >= 192 ? 1 : -1;
        }
    }

    internal sealed class MeasurementSeries
    {
        // The 30-second experiment has about 1,875 captures. Keep all expected
        // samples, but fail explicitly rather than silently truncate a bad loop.
        private readonly List<double> values = new List<double>();
        internal int Count { get { return values.Count; } }
        internal void Add(double value)
        {
            if (Double.IsNaN(value) || Double.IsInfinity(value) || value < 0 || values.Count >= 32768)
                throw new SmokeFailure("benchmark", "invalid_measurement");
            values.Add(value);
        }
        internal double[] Values() { return values.ToArray(); }
        internal object Summary()
        {
            if (values.Count == 0) return null;
            double[] sorted = values.ToArray();
            Array.Sort(sorted);
            double total = 0;
            foreach (double value in sorted) total += value;
            return new { count = sorted.Length, average = total / sorted.Length,
                p50 = sorted[(int)Math.Ceiling(sorted.Length * 0.50) - 1],
                p95 = sorted[(int)Math.Ceiling(sorted.Length * 0.95) - 1], max = sorted[sorted.Length - 1] };
        }
    }

    internal sealed class PerformanceMeasurement
    {
        internal const int WarmupSeconds = 5;
        internal const int MeasurementSeconds = 30;
        internal readonly MeasurementSeries InputLatency = new MeasurementSeries();
        internal readonly MeasurementSeries Capture = new MeasurementSeries();
        internal readonly MeasurementSeries SampleInterval = new MeasurementSeries();
        internal double FirstValidAt = -1, StartedAt = -1, LastFrameAt = -1;
        internal int Samples, ValidSamples, InvalidSamples, VisibleUpdates, SequenceGaps, StallEpisodes;
        internal int ProbesSent, ResponsesReceived;
        internal double MaxGapMs;
        private int lastSequence = -1;
        private byte currentEpoch, expectedEpoch;
        private bool pending, gapStalled;
        private double sentAt, previousCaptureStart = -1;

        internal bool Measuring { get { return StartedAt >= 0; } }
        internal bool Pending { get { return pending; } }
        internal double Elapsed(double now) { return Measuring ? Math.Max(0, now - StartedAt) : 0; }

        internal void Observe(bool valid, PerformanceMarker marker, double captureStart, double now)
        {
            if (valid && lastSequence >= 0 && marker.Sequence < lastSequence) valid = false;
            if (valid && FirstValidAt < 0) FirstValidAt = now;
            if (!Measuring)
            {
                if (!valid || now - FirstValidAt < WarmupSeconds) return;
                StartedAt = LastFrameAt = now;
                lastSequence = marker.Sequence;
                currentEpoch = marker.InputEpoch;
            }
            Samples++;
            Capture.Add((now - captureStart) * 1000);
            if (previousCaptureStart >= 0) SampleInterval.Add((captureStart - previousCaptureStart) * 1000);
            previousCaptureStart = captureStart;
            if (!valid) { InvalidSamples++; TrackGap(now); return; }
            ValidSamples++;
            if (marker.Sequence > lastSequence)
            {
                TrackGap(now);
                SequenceGaps += marker.Sequence - lastSequence - 1;
                VisibleUpdates++;
                lastSequence = marker.Sequence;
                LastFrameAt = now;
                gapStalled = false;
            }
            else TrackGap(now);
            if (pending && marker.InputEpoch == expectedEpoch)
            {
                InputLatency.Add((now - sentAt) * 1000);
                ResponsesReceived++;
                pending = false;
                currentEpoch = marker.InputEpoch;
            }
            else if (marker.InputEpoch != currentEpoch)
                throw new SmokeFailure("benchmark", "unexpected_input_epoch");
        }

        private void TrackGap(double now)
        {
            double gap = (now - LastFrameAt) * 1000;
            MaxGapMs = Math.Max(MaxGapMs, gap);
            if (gap >= 250 && !gapStalled) { StallEpisodes++; gapStalled = true; }
        }

        internal bool ShouldProbe(double now)
        {
            return Measuring && !pending && ProbesSent < 6 && Elapsed(now) < MeasurementSeconds &&
                Elapsed(now) >= ProbesSent * 5;
        }

        internal void ProbeSent(double now)
        {
            if (!ShouldProbe(now)) throw new SmokeFailure("benchmark", "overlapping_input_probe");
            expectedEpoch = (byte)((currentEpoch + 1) & 255);
            sentAt = now;
            pending = true;
            ProbesSent++;
        }

        internal bool ProbeTimedOut(double now) { return pending && now - sentAt > 10; }
        internal bool Complete(double now)
        {
            return Elapsed(now) >= MeasurementSeconds && VisibleUpdates > 0 && ResponsesReceived >= 3 &&
                ResponsesReceived == ProbesSent && !pending && now - LastFrameAt <= 5;
        }

        internal object Report(string outcome, string reason, Size captureSize, double now)
        {
            double elapsed = Elapsed(now);
            return new { version = 1, workload = "performance", outcome = outcome, reason = reason,
                measurementComplete = outcome == "passed" && Complete(now), warmupSeconds = WarmupSeconds,
                measurementSeconds = elapsed, sourceWidth = 1920, sourceHeight = 1080,
                captureWidth = captureSize.Width, captureHeight = captureSize.Height,
                requestedCaptureIntervalMs = 16, sampleCount = Samples, validSamples = ValidSamples,
                invalidSamples = InvalidSamples, visibleUpdates = VisibleUpdates,
                sampledUpdatesPerSecond = elapsed > 0 ? (double?)VisibleUpdates / elapsed : null,
                unobservedSequenceNumbers = SequenceGaps,
                maxVisibleGapMs = Measuring ? (double?)Math.Max(MaxGapMs, (now - LastFrameAt) * 1000) : null,
                stallEpisodes = StallEpisodes, inputProbesSent = ProbesSent, inputResponsesReceived = ResponsesReceived,
                inputLatencyMs = InputLatency.Summary(), captureMs = Capture.Summary(), sampleIntervalMs = SampleInterval.Summary(),
                inputLatencySamplesMs = InputLatency.Values(),
                notes = new[] { "Visible updates are sampled screen progress, not exact presentation FPS. Sequence gaps are unobserved frames, not proof of dropped frames.",
                    "Input latency includes server frame scheduling, RDP, client rendering, and capture sampling. It is not pure network RTT.",
                    "Stall episodes count observed gaps of at least 250 ms. Client capture and smart sizing add measurement overhead.",
                    "CPU and memory are measured separately by the orchestrator; this loopback test does not exercise KVM or a WAN." } };
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

    internal static class ClientDiagnostics
    {
        internal static string LocalDisconnectDescription(bool localFixture, Func<string> describe)
        {
            // Only the generated loopback fixture may retain Microsoft's error
            // description. External gateway reports keep numeric codes only.
            if (!localFixture) return null;
            try
            {
                string description = describe();
                if (String.IsNullOrWhiteSpace(description)) return null;
                description = description.Trim();
                return description.Substring(0, Math.Min(description.Length, 1024));
            }
            catch { return null; } // Preserve the original disconnect evidence.
        }
    }

    internal static class RdpKeyboard
    {
        internal static int SendF8(Func<bool> focus, Func<int, short[], int[], int> send)
        {
            // An active top-level form does not establish that the hosted
            // ActiveX child has keyboard focus. Verify it before injection.
            if (!focus()) throw new SmokeFailure("infrastructure", "rdp_control_not_focused");
            // Use raw scan codes, as Microsoft's RDCMan SendKeys caller does:
            // it passes MapVirtualKey(..., MAPVK_VK_TO_VSC) unchanged for both
            // edges. The reference page's WM_KEYDOWN-lParam wording is
            // misleading here: 0x00420001 puts Escape (0x01) in the low byte.
            // F8 is scan code 0x42; the separate VARIANT_BOOL array marks key-up.
            // https://learn.microsoft.com/sysinternals/downloads/rdcman
            int status = send(2, new short[] { 0, -1 }, new[] { 0x42, 0x42 });
            if (status < 0) throw new COMException("RDP SendKeys failed.", status);
            // Preserve nonnegative statuses too. Only the changed remote
            // picture can establish that the key reached the server.
            return status;
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
        private readonly bool performance;
        private readonly PerformanceMeasurement measurement = new PerformanceMeasurement();
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

        internal SmokeWindow(Profile profile, string password, string output, int duration, Report report, bool localFixture, bool performance)
        {
            this.profile = profile; this.password = password; this.output = output;
            this.duration = duration; this.report = report;
            this.localFixture = localFixture;
            this.performance = performance;
            AutoScaleMode = AutoScaleMode.None;
            ClientSize = performance ? PerformanceCaptureSize(Screen.PrimaryScreen.Bounds.Size) : new Size(1024, 768);
            FormBorderStyle = FormBorderStyle.None;
            StartPosition = FormStartPosition.Manual;
            Location = Screen.PrimaryScreen.Bounds.Location;
            TopMost = true;
            Text = performance ? "DevBox RDP performance baseline" : "DevBox RDP compatibility smoke test";
            BackColor = Color.Magenta;
            Paint += delegate(object sender, PaintEventArgs e) {
                if (!started) e.Graphics.FillRectangle(Brushes.Cyan, 0, 0, ClientSize.Width / 2, ClientSize.Height);
            };
            poll.Interval = performance ? 16 : 250;
            poll.Tick += Poll;
            Shown += delegate { Activate(); poll.Start(); };
            FormClosing += delegate { if (!finishing) Fail("client", "window_closed", null); };
        }

        internal static Size PerformanceCaptureSize(Size available)
        {
            int scale = Math.Min(120, Math.Min(available.Width / 16, available.Height / 9));
            if (scale < 40) throw new SmokeFailure("infrastructure", "desktop_too_small");
            return new Size(scale * 16, scale * 9);
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
            report.Add("infrastructure", "passed", null, new { desktopWidth = ClientSize.Width, desktopHeight = ClientSize.Height });
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
            client.DesktopWidth = performance ? 1920 : 1024;
            client.DesktopHeight = performance ? 1080 : 768;
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
            advanced.SmartSizing = performance;
            advanced.GrabFocusOnConnect = true;
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
                audioMode = profile.AudioMode, credentialPromptOverride = true, localFixture = localFixture,
                workload = performance ? "performance" : "smoke", smartSizing = performance });
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
                double captureStart = clock.Elapsed.TotalSeconds;
                using (Bitmap image = CaptureContent())
                {
                    if (performance)
                    {
                        PollPerformance(image, connected, captureStart, clock.Elapsed.TotalSeconds);
                        return;
                    }
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
                            var keys = (INonScriptable3)ocx;
                            int inputStatus;
                            try { inputStatus = RdpKeyboard.SendF8(FocusClient, keys.SendKeys); }
                            catch (COMException error)
                            { Fail("input", "sendkeys_failed", new { hresult = error.HResult }); return; }
                            keyAt = now;
                            report.Add("input", "progress", "f8_submitted", new {
                                hresult = inputStatus, controlFocused = true,
                                keyEncoding = "scan_code",
                                method = "IMsRdpClientNonScriptable.SendKeys" });
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

        private void PollPerformance(Bitmap image, int connected, double captureStart, double now)
        {
            PerformanceMarker marker;
            bool valid = PerformanceMarker.TryRead(image, out marker);
            if (connected != 1)
            {
                if (measurement.FirstValidAt >= 0) Fail("connection", "connection_state_lost", null);
                else if (now - connectAt > 45) Fail("render", "fixture_not_visible", null);
                return;
            }
            bool first = measurement.FirstValidAt < 0 && valid;
            bool measuring = measurement.Measuring;
            measurement.Observe(valid, marker, captureStart, now);
            if (first)
            {
                Save(image, "before-input.png", false);
                report.Add("render", "passed", "fixture_visible", new { workload = "performance", sequence = marker.Sequence });
            }
            if (!measurement.Measuring)
            {
                if (now - connectAt > 45) Fail("render", "fixture_not_visible", null);
                return;
            }
            if (!measuring) report.Add("benchmark", "progress", "measurement_started", new {
                warmupSeconds = PerformanceMeasurement.WarmupSeconds, requiredSeconds = PerformanceMeasurement.MeasurementSeconds });
            if (valid && measurement.ShouldProbe(clock.Elapsed.TotalSeconds))
            {
                var keys = (INonScriptable3)ocx;
                double submittedAt = clock.Elapsed.TotalSeconds;
                measurement.ProbeSent(submittedAt);
                int status = RdpKeyboard.SendF8(FocusClient, keys.SendKeys);
                report.Add("input", "progress", "benchmark_f8_submitted", new {
                    probe = measurement.ProbesSent, hresult = status, keyEncoding = "scan_code" });
            }
            if (measurement.ProbeTimedOut(now)) { Fail("input", "benchmark_input_timeout", null); return; }
            if (measurement.Elapsed(now) >= PerformanceMeasurement.MeasurementSeconds && !measurement.Pending)
            {
                if (!measurement.Complete(now)) { Fail("benchmark", "insufficient_observations", null); return; }
                SavePerformance("passed", "performance_measurement_completed", now);
                Save(image, "terminal.png", !valid);
                report.Add("benchmark", "passed", "performance_measurement_completed", new {
                    measurementSeconds = measurement.Elapsed(now), inputResponsesReceived = measurement.ResponsesReceived,
                    visibleUpdates = measurement.VisibleUpdates });
                Finish(0);
            }
        }

        private void SavePerformance(string outcome, string reason, double now)
        {
            File.WriteAllText(Path.Combine(output, "client-performance.json"),
                new JavaScriptSerializer().Serialize(measurement.Report(outcome, reason, ClientSize, now)));
        }

        private bool FocusClient()
        {
            Activate();
            host.Select();
            host.Focus();
            // Focus may be on a native child inside the AxHost window.
            return host.ContainsFocus;
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
            string description = ClientDiagnostics.LocalDisconnectDescription(localFixture && extended.HasValue, delegate {
                dynamic client = ocx;
                return (string)client.GetErrorDescription(unchecked((uint)reason), unchecked((uint)extended.Value));
            });
            var details = new { disconnectReason = reason, extendedDisconnectReason = extended, description = description };
            if (finishing) report.Add("connection", "closed", "intentional_disconnect", details);
            else Fail("connection", "unexpected_disconnect", details);
        }

        internal void Fail(string stage, string reason, object details)
        {
            if (finishing) return;
            report.Add(stage, "failed", reason, details);
            if (performance)
            {
                try { SavePerformance("failed", reason, clock.Elapsed.TotalSeconds); }
                catch { report.Add("artifact", "unavailable", "performance_report_failed", null); }
            }
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
        [PreserveSig]
        int SendKeys(int count, [In, MarshalAs(UnmanagedType.LPArray, SizeParamIndex = 0)] short[] keyUp,
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
                string workload = Environment.GetEnvironmentVariable("RDP_SMOKE_WORKLOAD");
                if (workload != null && workload != "smoke" && workload != "performance")
                    throw new SmokeFailure("configuration", "invalid_workload");
                bool performance = workload == "performance";
                if (performance && (!localFixture || duration != PerformanceMeasurement.MeasurementSeconds))
                    throw new SmokeFailure("configuration", "performance_requires_local_fixture");
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
                using (var window = new SmokeWindow(profile, password, output, duration, report, localFixture, performance))
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
            KeyboardInjectionTest();
            PerformanceTest();
            bool externalDescriptionRead = false;
            Check(ClientDiagnostics.LocalDisconnectDescription(false, delegate { externalDescriptionRead = true; return "private description"; }) == null);
            Check(!externalDescriptionRead);
            Check(ClientDiagnostics.LocalDisconnectDescription(true, delegate { return " fixture failure "; }) == "fixture failure");
            Check(ClientDiagnostics.LocalDisconnectDescription(true, delegate { return new string('x', 2048); }).Length == 1024);
            Check(ClientDiagnostics.LocalDisconnectDescription(true, delegate { return null; }) == null);
            Check(ClientDiagnostics.LocalDisconnectDescription(true, delegate { return " \r\n "; }) == null);
            Check(ClientDiagnostics.LocalDisconnectDescription(true, delegate { throw new Exception("description unavailable"); }) == null);
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

        private static void PerformanceTest()
        {
            Check(SmokeWindow.PerformanceCaptureSize(new Size(1024, 768)) == new Size(1024, 576));
            Check(SmokeWindow.PerformanceCaptureSize(new Size(1920, 1080)) == new Size(1920, 1080));
            bool rejected = false;
            try { SmokeWindow.PerformanceCaptureSize(new Size(600, 400)); }
            catch (SmokeFailure) { rejected = true; }
            Check(rejected);
            // Independent golden packet: sequence 0x123456, epoch 7,
            // checksum 0x12 ^ 0x34 ^ 0x56 ^ 7 ^ 0xa5 = 0xd2.
            foreach (Size size in new[] { new Size(1920, 1080), new Size(1024, 576), new Size(640, 360) })
            {
                using (var image = new Bitmap(size.Width, size.Height))
                using (Graphics graphics = Graphics.FromImage(image))
                {
                    graphics.Clear(Color.Gray);
                    PaintPerformanceMarker(graphics, size, 0xd207123456UL);
                    PerformanceMarker marker;
                    Check(PerformanceMarker.TryRead(image, out marker) && marker.Sequence == 0x123456 && marker.InputEpoch == 7);
                    PaintPerformanceMarker(graphics, size, 0xd307123456UL); // Complement is valid, checksum is not.
                    Check(!PerformanceMarker.TryRead(image, out marker));
                    graphics.Clear(Color.Black);
                    Check(!PerformanceMarker.TryRead(image, out marker));
                    PaintPerformanceMarker(graphics, size, 0xd207123456UL);
                    graphics.FillRectangle(Brushes.Lime, 64 * size.Width / 1920, 16 * size.Height / 1080,
                        32 * size.Width / 1920, 32 * size.Height / 1080);
                    Check(!PerformanceMarker.TryRead(image, out marker));
                }
            }
            var series = new MeasurementSeries();
            Check(series.Summary() == null);
            foreach (double value in new[] { 100.0, 3, 1, 4, 2 }) series.Add(value);
            var json = new JavaScriptSerializer();
            var summary = json.Deserialize<Dictionary<string, object>>(json.Serialize(series.Summary()));
            Check(Convert.ToDouble(summary["average"], CultureInfo.InvariantCulture) == 22 &&
                Convert.ToDouble(summary["p50"], CultureInfo.InvariantCulture) == 3 &&
                Convert.ToDouble(summary["p95"], CultureInfo.InvariantCulture) == 100);
            foreach (double invalid in new[] { Double.NaN, Double.PositiveInfinity, -1.0 })
            {
                rejected = false;
                try { series.Add(invalid); } catch (SmokeFailure) { rejected = true; }
                Check(rejected);
            }
            var measurement = new PerformanceMeasurement();
            measurement.Observe(true, new PerformanceMarker { Sequence = 10 }, 0.99, 1);
            measurement.Observe(false, new PerformanceMarker(), 5.99, 6);
            Check(!measurement.Measuring);
            measurement.Observe(true, new PerformanceMarker { Sequence = 11 }, 5.999, 6);
            Check(measurement.Measuring && measurement.Samples == 1 && measurement.ShouldProbe(6));
            measurement.ProbeSent(6.01);
            Check(!measurement.ShouldProbe(11) && !measurement.Complete(36));
            measurement.Observe(true, new PerformanceMarker { Sequence = 12, InputEpoch = 1 }, 6.03, 6.04);
            Check(measurement.ResponsesReceived == 1 && Math.Abs(measurement.InputLatency.Values()[0] - 30) < 0.001);
            Check(!measurement.ShouldProbe(10) && measurement.ShouldProbe(11));
            measurement.ProbeSent(11);
            measurement.Observe(true, new PerformanceMarker { Sequence = 14, InputEpoch = 2 }, 11.09, 11.1);
            Check(measurement.SequenceGaps == 1 && measurement.StallEpisodes == 1);
            measurement.ProbeSent(16);
            Check(measurement.ProbeTimedOut(26.01));
            measurement.Observe(true, new PerformanceMarker { Sequence = 15, InputEpoch = 3 }, 16.09, 16.1);
            measurement.Observe(true, new PerformanceMarker { Sequence = 16, InputEpoch = 3 }, 35.99, 36);
            Check(measurement.Complete(36) && !measurement.ShouldProbe(36) && measurement.ResponsesReceived == 3);
            measurement.Observe(true, new PerformanceMarker { Sequence = 15, InputEpoch = 3 }, 36.01, 36.02);
            Check(measurement.InvalidSamples == 1 && measurement.VisibleUpdates == 4);
            rejected = false;
            try { measurement.Observe(true, new PerformanceMarker { Sequence = 17, InputEpoch = 4 }, 36.03, 36.04); }
            catch (SmokeFailure) { rejected = true; }
            Check(rejected);
            var failed = json.Deserialize<Dictionary<string, object>>(json.Serialize(
                new PerformanceMeasurement().Report("failed", "test", new Size(1024, 576), 0)));
            Check((bool)failed["measurementComplete"] == false && failed["inputLatencyMs"] == null &&
                failed["sampledUpdatesPerSecond"] == null);
        }

        private static void PaintPerformanceMarker(Graphics graphics, Size size, ulong packet)
        {
            for (int bit = 0; bit < 40; bit++)
                for (int row = 0; row < 2; row++)
                {
                    bool white = ((packet >> bit) & 1) != 0;
                    if (row == 1) white = !white;
                    int x = (64 + bit * 32) * size.Width / 1920;
                    int right = (96 + bit * 32) * size.Width / 1920;
                    int y = (16 + row * 40) * size.Height / 1080;
                    int bottom = (48 + row * 40) * size.Height / 1080;
                    graphics.FillRectangle(white ? Brushes.White : Brushes.Black, x, y, right - x, bottom - y);
                }
        }

        private static void KeyboardInjectionTest()
        {
            bool focused = false, sent = false;
            int status = RdpKeyboard.SendF8(delegate { focused = true; return true; },
                delegate(int count, short[] keyUp, int[] keyData) {
                    Check(focused && count == 2 && keyUp.Length == count && keyData.Length == count);
                    Check(keyUp[0] == 0 && keyUp[1] == -1); // Native VARIANT_FALSE/VARIANT_TRUE.
                    for (int i = 0; i < count; i++)
                    {
                        // Native SendKeys consumes the scan code itself, with
                        // release in keyUp. Reject the old LPARAM encoding,
                        // whose low byte 0x01 identifies Escape instead of F8.
                        Check((keyData[i] & 0xff) == 0x42 && (keyData[i] & ~0xff) == 0);
                    }
                    Check(keyData[0] == keyData[1]);
                    sent = true;
                    return 0;
                });
            Check(sent && status == 0);
            sent = false;
            bool rejected = false;
            try
            {
                RdpKeyboard.SendF8(delegate { return false; }, delegate(int count, short[] keyUp, int[] keyData) {
                    sent = true; return 0;
                });
            }
            catch (SmokeFailure error)
            { rejected = error.Stage == "infrastructure" && error.Reason == "rdp_control_not_focused"; }
            Check(rejected && !sent);
            int failure = unchecked((int)0x80004005);
            rejected = false;
            try
            {
                RdpKeyboard.SendF8(delegate { return true; }, delegate(int count, short[] keyUp, int[] keyData) { return failure; });
            }
            catch (COMException error) { rejected = error.HResult == failure; }
            Check(rejected);
            // S_FALSE must remain visible in diagnostics, not disappear through
            // automatic HRESULT marshaling or imply remote input was received.
            Check(RdpKeyboard.SendF8(delegate { return true; },
                delegate(int count, short[] keyUp, int[] keyData) { return 1; }) == 1);
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
