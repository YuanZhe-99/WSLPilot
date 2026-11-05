package configdoc

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Kind is a setting's value type.
type Kind int

// Value kinds.
const (
	Bool Kind = iota
	Int
	Size // integer with an optional unit, e.g. 8GB
	Text
	Path // a Windows path
	Enum
)

// Effect says what must happen before a change applies.
type Effect int

// Effects of a change.
const (
	RestartDistro  Effect = iota // wsl --terminate <distro>
	RestartWSL                   // wsl --shutdown
	RestartSession               // wslc system session terminate
	NextCommand                  // read by each wslc command
)

// Field is one setting the forms show.
// Notes: keys and defaults are copied from Microsoft Learn ("Advanced
// settings configuration in WSL" and "WSL container settings", both dated
// 2026-10-16); doc/en-us/upstream.md records the source.
type Field struct {
	Section string // INI section, or YAML parent path ("" for top level)
	Key     string
	Kind    Kind
	Default string // as WSL documents it; "" when it depends on the machine
	Choices []string
	Effect  Effect
	// ID names the catalog messages: Cfg<ID> is the label, Cfg<ID>Help
	// the explanation.
	ID string
	// EscapeBackslashes: .wslconfig paths must double their backslashes.
	EscapeBackslashes bool
}

// Path is the field's dotted name, e.g. "wsl2.memory".
func (f Field) Path() string {
	if f.Section == "" {
		return f.Key
	}
	return f.Section + "." + f.Key
}

var sizeRe = regexp.MustCompile(`(?i)^[0-9]+\s*(b|k|kb|m|mb|g|gb|t|tb|p|pb)?$`)

// Validate checks a value typed in the form ("" means "use the default"
// and is always valid).
func (f Field) Validate(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	switch f.Kind {
	case Bool:
		if v != "true" && v != "false" {
			return fmt.Errorf("%s: expected true or false", f.Path())
		}
	case Int:
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Errorf("%s: expected a whole number", f.Path())
		}
	case Size:
		if !sizeRe.MatchString(v) {
			return fmt.Errorf("%s: expected a size such as 8GB", f.Path())
		}
	case Enum:
		if !slices.ContainsFunc(f.Choices, func(c string) bool { return strings.EqualFold(c, v) }) {
			return fmt.Errorf("%s: expected one of %s", f.Path(), strings.Join(f.Choices, ", "))
		}
	}
	return nil
}

// Encode turns a form value into the text written to the file.
func (f Field) Encode(v string) string {
	v = strings.TrimSpace(v)
	if f.EscapeBackslashes && f.Kind == Path {
		v = strings.ReplaceAll(strings.ReplaceAll(v, `\\`, `\`), `\`, `\\`)
	}
	return v
}

// Decode turns file text into the form value.
func (f Field) Decode(v string) string {
	v = Unquote(strings.TrimSpace(v))
	if f.EscapeBackslashes && f.Kind == Path {
		v = strings.ReplaceAll(v, `\\`, `\`)
	}
	return v
}

// WSLConf lists the /etc/wsl.conf settings the distribution form shows.
var WSLConf = []Field{
	{Section: "boot", Key: "systemd", Kind: Bool, Default: "false", ID: "BootSystemd"},
	{Section: "boot", Key: "command", Kind: Text, ID: "BootCommand"},
	{Section: "user", Key: "default", Kind: Text, ID: "UserDefault"},
	{Section: "network", Key: "hostname", Kind: Text, ID: "NetHostname"},
	{Section: "network", Key: "generateHosts", Kind: Bool, Default: "true", ID: "NetGenerateHosts"},
	{Section: "network", Key: "generateResolvConf", Kind: Bool, Default: "true", ID: "NetGenerateResolv"},
	{Section: "interop", Key: "enabled", Kind: Bool, Default: "true", ID: "InteropEnabled"},
	{Section: "interop", Key: "appendWindowsPath", Kind: Bool, Default: "true", ID: "InteropPath"},
	{Section: "automount", Key: "enabled", Kind: Bool, Default: "true", ID: "MountEnabled"},
	{Section: "automount", Key: "root", Kind: Text, Default: "/mnt/", ID: "MountRoot"},
	{Section: "automount", Key: "options", Kind: Text, ID: "MountOptions"},
	{Section: "automount", Key: "mountFsTab", Kind: Bool, Default: "true", ID: "MountFsTab"},
	{Section: "time", Key: "useWindowsTimezone", Kind: Bool, Default: "true", ID: "TimeZone"},
	{Section: "gpu", Key: "enabled", Kind: Bool, Default: "true", ID: "GPUEnabled"},
}

// WSLConfig lists the %UserProfile%\.wslconfig settings the WSL form shows.
var WSLConfig = []Field{
	{Section: "wsl2", Key: "memory", Kind: Size, Effect: RestartWSL, ID: "Memory"},
	{Section: "wsl2", Key: "processors", Kind: Int, Effect: RestartWSL, ID: "Processors"},
	{Section: "wsl2", Key: "swap", Kind: Size, Effect: RestartWSL, ID: "Swap"},
	{Section: "wsl2", Key: "swapFile", Kind: Path, Effect: RestartWSL, ID: "SwapFile", EscapeBackslashes: true},
	{Section: "wsl2", Key: "networkingMode", Kind: Enum, Default: "nat", Choices: []string{"nat", "mirrored", "consomme", "none"}, Effect: RestartWSL, ID: "NetworkingMode"},
	{Section: "wsl2", Key: "localhostForwarding", Kind: Bool, Default: "true", Effect: RestartWSL, ID: "LocalhostForwarding"},
	{Section: "wsl2", Key: "dnsTunneling", Kind: Bool, Default: "true", Effect: RestartWSL, ID: "DNSTunneling"},
	{Section: "wsl2", Key: "firewall", Kind: Bool, Default: "true", Effect: RestartWSL, ID: "Firewall"},
	{Section: "wsl2", Key: "autoProxy", Kind: Bool, Default: "true", Effect: RestartWSL, ID: "AutoProxy"},
	{Section: "wsl2", Key: "guiApplications", Kind: Bool, Default: "true", Effect: RestartWSL, ID: "GUIApps"},
	{Section: "wsl2", Key: "nestedVirtualization", Kind: Bool, Default: "true", Effect: RestartWSL, ID: "Nested"},
	{Section: "wsl2", Key: "vmIdleTimeout", Kind: Int, Default: "60000", Effect: RestartWSL, ID: "VMIdle"},
	{Section: "wsl2", Key: "defaultVhdSize", Kind: Size, Default: "1099511627776", Effect: RestartWSL, ID: "DefaultVHDSize"},
	{Section: "wsl2", Key: "kernel", Kind: Path, Effect: RestartWSL, ID: "Kernel", EscapeBackslashes: true},
	{Section: "general", Key: "instanceIdleTimeout", Kind: Int, Default: "15000", Effect: RestartWSL, ID: "InstanceIdle"},
	{Section: "general", Key: "distributionInstallPath", Kind: Path, Effect: RestartWSL, ID: "InstallPath", EscapeBackslashes: true},
	{Section: "experimental", Key: "autoMemoryReclaim", Kind: Enum, Default: "dropCache", Choices: []string{"disabled", "gradual", "dropCache"}, Effect: RestartWSL, ID: "MemoryReclaim"},
	{Section: "experimental", Key: "sparseVhd", Kind: Bool, Default: "false", Effect: RestartWSL, ID: "SparseVHD"},
	{Section: "experimental", Key: "hostAddressLoopback", Kind: Bool, Default: "false", Effect: RestartWSL, ID: "HostLoopback"},
}

// WSLcSettings lists the %LOCALAPPDATA%\wslc\settings.yaml settings.
// Notes: every setting also accepts the literal "default".
var WSLcSettings = []Field{
	{Section: "session", Key: "cpuCount", Kind: Int, Effect: RestartSession, ID: "SessionCPU"},
	{Section: "session", Key: "memorySize", Kind: Size, Effect: RestartSession, ID: "SessionMemory"},
	{Section: "session", Key: "maxStorageSize", Kind: Size, Default: "1TB", Effect: RestartSession, ID: "SessionStorageSize"},
	{Section: "session", Key: "storagePath", Kind: Path, Effect: RestartSession, ID: "SessionStoragePath"},
	{Section: "session", Key: "defaultBindingAddress", Kind: Text, Default: "127.0.0.1", Effect: NextCommand, ID: "SessionBinding"},
	{Section: "session", Key: "hostLoopback", Kind: Text, Default: "host.wslc.internal", Effect: RestartSession, ID: "SessionHostLoopback"},
	{Section: "session", Key: "idleTimeout", Kind: Int, Default: "30", Effect: RestartSession, ID: "SessionIdle"},
	{Key: "credentialStore", Kind: Enum, Default: "wincred", Choices: []string{"wincred", "file"}, Effect: NextCommand, ID: "CredentialStore"},
}

// Known reports whether section.key is one of fields (case-insensitive).
func Known(fields []Field, section, key string) bool {
	return slices.ContainsFunc(fields, func(f Field) bool {
		return strings.EqualFold(f.Section, section) && strings.EqualFold(f.Key, key)
	})
}

// Change is one pending edit from a form.
type Change struct {
	Field Field
	Old   string // decoded value before ("" = default)
	New   string // "" removes the key (back to the default)
}

// ApplyINI applies changes to an INI document, validating first.
// Returns: the first validation error, leaving doc untouched.
func ApplyINI(doc *INI, changes []Change) error {
	for _, c := range changes {
		if err := c.Field.Validate(c.New); err != nil {
			return err
		}
	}
	for _, c := range changes {
		if strings.TrimSpace(c.New) == "" {
			doc.Unset(c.Field.Section, c.Field.Key)
		} else {
			doc.Set(c.Field.Section, c.Field.Key, c.Field.Encode(c.New))
		}
	}
	return nil
}

// ApplyYAML applies changes to a YAML document, validating first; an
// empty value is written as WSLc's "default".
func ApplyYAML(doc *YAML, changes []Change) error {
	for _, c := range changes {
		if c.New != "default" {
			if err := c.Field.Validate(c.New); err != nil {
				return err
			}
		}
	}
	for _, c := range changes {
		v := strings.TrimSpace(c.New)
		if v == "" {
			v = "default"
		}
		doc.Set(c.Field.Path(), QuoteYAML(c.Field.Encode(v)))
	}
	return doc.Validate()
}

// DiffLines returns a minimal unified-style preview: lines removed ("-")
// and added ("+") between before and after, in order.
func DiffLines(before, after string) []string {
	a := strings.Split(strings.TrimSuffix(strings.ReplaceAll(before, "\r\n", "\n"), "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(strings.ReplaceAll(after, "\r\n", "\n"), "\n"), "\n")
	// longest common subsequence table
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	return out
}
