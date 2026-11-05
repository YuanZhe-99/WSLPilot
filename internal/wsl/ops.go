package wsl

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/YuanZhe-99/WSLPilot/internal/procexec"
)

// InstallOptions are the `wsl --install` options WSLPilot offers.
type InstallOptions struct {
	Name        string // --name: register under another name
	Location    string // --location: folder for the disk
	Version     int    // --version 1|2 (0: WSL's default)
	FixedVHD    bool   // --fixed-vhd
	VHDSize     string // --vhd-size, e.g. "256GB"
	WebDownload bool   // --web-download: bypass the Microsoft Store
	FromFile    string // --from-file: a local .wsl image instead of a name
}

// InstallArgs builds the argument vector for installing distro.
// Notes: --no-launch is always passed: the first-run user setup happens in
// WSLPilot's terminal, never in a console window WSLPilot cannot see.
func InstallArgs(distro string, o InstallOptions) []string {
	a := []string{"--install"}
	if o.FromFile != "" {
		a = append(a, "--from-file", o.FromFile)
	} else {
		a = append(a, distro)
	}
	a = append(a, "--no-launch")
	if o.Name != "" {
		a = append(a, "--name", o.Name)
	}
	if o.Location != "" {
		a = append(a, "--location", o.Location)
	}
	if o.Version != 0 {
		a = append(a, "--version", strconv.Itoa(o.Version))
	}
	if o.FixedVHD {
		a = append(a, "--fixed-vhd")
	}
	if o.VHDSize != "" {
		a = append(a, "--vhd-size", o.VHDSize)
	}
	if o.WebDownload {
		a = append(a, "--web-download")
	}
	return a
}

// Install runs `wsl --install`, streaming wsl.exe's progress text to log.
func (c *Client) Install(ctx context.Context, distro string, o InstallOptions, log io.Writer) error {
	return c.stream(ctx, InstallArgs(distro, o), nil, log)
}

// ImportOptions are the `wsl --import` options.
type ImportOptions struct {
	Version int  // 1|2, 0 for the default
	VHD     bool // the file is a .vhd/.vhdx (copied to the location)
}

// Import registers a new distribution from a tar or VHD file.
// Inputs: file may be "-" with r supplying the tar stream.
func (c *Client) Import(ctx context.Context, name, location, file string, o ImportOptions, r io.Reader, log io.Writer) error {
	a := []string{"--import", name, location, file}
	if o.Version != 0 {
		a = append(a, "--version", strconv.Itoa(o.Version))
	}
	if o.VHD {
		a = append(a, "--vhd")
	}
	return c.stream(ctx, a, r, log)
}

// ImportInPlace registers an existing ext4 VHDX where it lies.
func (c *Client) ImportInPlace(ctx context.Context, name, vhdx string) error {
	_, err := c.Run(ctx, Exe, "--import-in-place", name, vhdx)
	return err
}

// ExportFormat is a `wsl --export --format` value.
type ExportFormat string

// Export formats listed by `wsl --help` on 3.0.3.0.
const (
	FormatTar   ExportFormat = "tar"
	FormatTarGz ExportFormat = "tar.gz"
	FormatTarXz ExportFormat = "tar.xz"
	FormatVHD   ExportFormat = "vhd"
)

// Export writes the distribution to file, or streams the tar to w when
// file is "-".
// Notes: WSL stops nothing by itself; callers terminate the distribution
// first for a consistent copy (behavior contract).
func (c *Client) Export(ctx context.Context, name, file string, f ExportFormat, w io.Writer) error {
	a := []string{"--export", name, file}
	if f != "" && f != FormatTar {
		a = append(a, "--format", string(f))
	}
	if w == nil {
		w = io.Discard
	}
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: a, Stdout: w})
}

// Unregister removes the distribution and deletes its disk.
// Notes: destructive; the jobs layer confirms and offers a backup first.
func (c *Client) Unregister(ctx context.Context, name string) error {
	_, err := c.Run(ctx, Exe, "--unregister", name)
	return err
}

// Start boots the distribution by running `true` in it.
func (c *Client) Start(ctx context.Context, name string) error {
	_, err := c.Run(ctx, Exe, ExecArgs(name, "", "true")...)
	return err
}

// Terminate stops one distribution.
func (c *Client) Terminate(ctx context.Context, name string) error {
	_, err := c.Run(ctx, Exe, "--terminate", name)
	return err
}

// Shutdown stops every distribution, WSL Containers and the utility VM.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.Run(ctx, Exe, "--shutdown")
	return err
}

// SetDefault makes name the default distribution.
func (c *Client) SetDefault(ctx context.Context, name string) error {
	_, err := c.Run(ctx, Exe, "--set-default", name)
	return err
}

// SetVersion converts the distribution between WSL 1 and 2 (a long job).
func (c *Client) SetVersion(ctx context.Context, name string, version int, log io.Writer) error {
	return c.stream(ctx, []string{"--set-version", name, strconv.Itoa(version)}, nil, log)
}

// SetDefaultVersion sets the version new distributions get.
func (c *Client) SetDefaultVersion(ctx context.Context, version int) error {
	_, err := c.Run(ctx, Exe, "--set-default-version", strconv.Itoa(version))
	return err
}

// Move moves the distribution's disk to location (`--manage --move`).
// Notes: verified on 3.0.3.0 to update BasePath in the registry.
func (c *Client) Move(ctx context.Context, name, location string, log io.Writer) error {
	return c.stream(ctx, []string{"--manage", name, "--move", location}, nil, log)
}

// Resize sets the virtual disk's maximum size, e.g. "512GB".
func (c *Client) Resize(ctx context.Context, name, size string) error {
	_, err := c.Run(ctx, Exe, "--manage", name, "--resize", size)
	return err
}

// SetSparse turns automatic space reclaim of the VHDX on or off.
// Notes: WSL warns that sparse VHDs are experimental (3.0.3.0).
func (c *Client) SetSparse(ctx context.Context, name string, on bool) error {
	_, err := c.Run(ctx, Exe, "--manage", name, "--set-sparse", strconv.FormatBool(on))
	return err
}

// SetDefaultUser sets the user wsl.exe logs in as.
func (c *Client) SetDefaultUser(ctx context.Context, name, user string) error {
	_, err := c.Run(ctx, Exe, "--manage", name, "--set-default-user", user)
	return err
}

// Compact shrinks the VHDX with `wsl --manage <distro> --compact`.
// Notes: verified on 3.0.3.0: needs no elevation and terminates the
// distribution if it runs. Older releases lack it; see SupportsCompact.
func (c *Client) Compact(ctx context.Context, name string, log io.Writer) error {
	return c.stream(ctx, []string{"--manage", name, "--compact"}, nil, log)
}

// SupportsCompact reports whether this wsl.exe has `--manage --compact`.
// Notes: option names in `wsl --help` are not localized, so the literal
// "--compact" is a reliable marker.
func (c *Client) SupportsCompact(ctx context.Context) bool {
	out, _ := c.Run(ctx, Exe, "--help")
	return strings.Contains(out, "--compact")
}

// Update runs `wsl --update` (optionally the pre-release channel).
func (c *Client) Update(ctx context.Context, preRelease bool, log io.Writer) error {
	a := []string{"--update"}
	if preRelease {
		a = append(a, "--pre-release")
	}
	return c.stream(ctx, a, nil, log)
}

// Exec runs a program inside the distribution and returns its stdout.
// Inputs: user "" means the distribution's default user. argv[0] is found
// on the distribution's PATH; no Linux shell interprets argv.
func (c *Client) Exec(ctx context.Context, name, user string, argv ...string) (string, error) {
	return c.Run(ctx, Exe, ExecArgs(name, user, argv...)...)
}

// ExecStream runs argv in the distribution with raw stdin/stdout streams
// (tar pipes).
func (c *Client) ExecStream(ctx context.Context, name, user string, stdin io.Reader, stdout io.Writer, argv ...string) error {
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: ExecArgs(name, user, argv...), Stdin: stdin, Stdout: stdout})
}

// ExecArgs builds `wsl --distribution name [--user user] --exec argv...`.
func ExecArgs(name, user string, argv ...string) []string {
	a := []string{"--distribution", name}
	if user != "" {
		a = append(a, "--user", user)
	}
	a = append(a, "--exec")
	return append(a, argv...)
}

// stream runs wsl.exe with optional stdin, sending its decoded stdout and
// stderr text to log.
func (c *Client) stream(ctx context.Context, args []string, stdin io.Reader, log io.Writer) error {
	if log == nil {
		log = io.Discard
	}
	dw := &decodeWriter{w: log}
	return c.Stream(ctx, procexec.Spec{Name: Exe, Args: args, Stdin: stdin, Stdout: dw, Stderr: dw})
}

// decodeWriter decodes wsl.exe text output chunk by chunk.
// Notes: with WSL_UTF8=1 the output is UTF-8; Decode also strips the NULs
// of an older release's UTF-16 so the log stays readable.
type decodeWriter struct{ w io.Writer }

// Write forwards the decoded text.
func (d *decodeWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(d.w, procexec.Decode(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// DiskUsage is `df` of the distribution's root filesystem.
type DiskUsage struct {
	SizeBytes, UsedBytes, AvailBytes int64
}

// DF runs `df -Pk /` in the distribution as root.
// Notes: POSIX output (-P) is the same in every locale and in busybox.
func (c *Client) DF(ctx context.Context, name string) (DiskUsage, error) {
	out, err := c.Exec(ctx, name, "root", "df", "-Pk", "/")
	if err != nil {
		return DiskUsage{}, err
	}
	return ParseDF(out)
}

// ParseDF reads the data line of `df -Pk`.
func ParseDF(out string) (DiskUsage, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	f := strings.Fields(lines[len(lines)-1])
	if len(f) < 4 {
		return DiskUsage{}, fmt.Errorf("wsl: unexpected df output %q", out)
	}
	n := func(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v * 1024 }
	return DiskUsage{SizeBytes: n(f[1]), UsedBytes: n(f[2]), AvailBytes: n(f[3])}, nil
}

// ReadFile returns a file of the distribution as root (wsl.conf).
// Returns: "" and no error when the file does not exist.
func (c *Client) ReadFile(ctx context.Context, name, path string) (string, bool, error) {
	out, err := c.Exec(ctx, name, "root", "sh", "-c", `if [ -e "$1" ]; then cat -- "$1"; else exit 3; fi`, "sh", path)
	if err != nil {
		if ee, ok := err.(*procexec.ExitError); ok && ee.Code == 3 {
			return "", false, nil
		}
		return "", false, err
	}
	return out, true, nil
}

// WriteFile replaces a file of the distribution as root, atomically
// (written beside it, then renamed), keeping a copy of the old file at
// backup when backup is not "".
func (c *Client) WriteFile(ctx context.Context, name, path, backup string, data []byte) error {
	script := `set -e; tmp="$1.wslpilot-new"; cat > "$tmp"; ` +
		`if [ -n "$2" ] && [ -e "$1" ]; then cp -p -- "$1" "$2"; fi; ` +
		`if [ -e "$1" ]; then chmod --reference="$1" "$tmp" 2>/dev/null || chmod 644 "$tmp"; else chmod 644 "$tmp"; fi; mv -f -- "$tmp" "$1"`
	return c.ExecStream(ctx, name, "root", strings.NewReader(string(data)), nil, "sh", "-c", script, "sh", path, backup)
}
