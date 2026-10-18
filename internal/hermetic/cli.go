package hermetic

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/aquaproj/aqua/v2/pkg/config"
	"github.com/aquaproj/aqua/v2/pkg/controller"
	aquaruntime "github.com/aquaproj/aqua/v2/pkg/runtime"
)

// grammar is the binary's grammar under its own name: the library's commands the
// toolset's scripts, recipes and workflows call and the dispatch uses, each
// with the flags it keeps, under the two global flags. The rest of aqua is not
// here: exec (a tool run through the library gets the library's environment,
// not the shell's; the links in the bin are how a tool runs), the verifier
// switches, the profiling flags, the deprecated flags, and the commands nothing
// here calls. A command or a flag outside the grammar fails as unknown.
type grammar struct {
	Config   string           `help:"Configuration file path."    placeholder:"PATH"           short:"c"`
	LogLevel string           `default:"info"                     enum:"debug,info,warn,error" env:"HERMETIC_LOG_LEVEL" help:"Log level."`
	Version  kong.VersionFlag `help:"Print the version and quit." short:"v"`

	Install        installCmd        `aliases:"i"   cmd:""                                                      help:"Install tools."`
	Update         updateCmd         `aliases:"up"  cmd:""                                                      help:"Update registries and packages."`
	UpdateChecksum updateChecksumCmd `aliases:"upc" cmd:""                                                      help:"Create or update aqua-checksums.json."                       name:"update-checksum"`
	Generate       generateCmd       `aliases:"g"   cmd:""                                                      help:"Search packages in registries and output the configuration."`
	Which          whichCmd          `cmd:""        help:"Output the absolute path of a tool."`
	Policy         policyCmd         `cmd:""        help:"Manage policy."`
	RootDir        rootDirCmd        `cmd:""        help:"Output the library's root directory (AQUA_ROOT_DIR)." name:"root-dir"`
	VersionCommand versionCmd        `cmd:""        help:"Show the version."                                    name:"version"`
}

// library is what a command runs with: the library's logger and runtime, the
// parameters the binary derived for the library, and the stream a command
// answers on. A command copies the parameters before it fills its own fields.
type library struct {
	logger  *slog.Logger
	root    Root
	runtime *aquaruntime.Runtime
	base    *config.Param
	stdout  io.Writer
	version string
}

func (lib *library) param() *config.Param {
	param := *lib.base

	return &param
}

// installCmd is `install`: the library's, under the flags the toolset uses.
type installCmd struct {
	OnlyLink    bool     `help:"Create links but skip downloading packages."      short:"l"`
	All         bool     `help:"Install the global configuration's packages too." short:"a"`
	Tags        string   `help:"Install only the packages having these tags."     short:"t"`
	ExcludeTags string   `help:"Install only the packages not having these tags."`
	Names       []string `arg:""                                                  help:"Command or package names to install; every one must exist." optional:""`
}

// Run installs through the library, then mirrors what it exposed.
func (c *installCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()
	param.OnlyLink = c.OnlyLink
	param.All = c.All
	param.Tags = parseTags(c.Tags)
	param.ExcludedTags = parseTags(c.ExcludeTags)
	param.Args = c.Names

	ctrl, err := controller.InitializeInstallCommandController(ctx, lib.logger, param, http.DefaultClient, lib.runtime)
	if err != nil {
		return fmt.Errorf("initialize the install controller: %w", err)
	}

	if err := ctrl.Install(ctx, lib.logger, param); err != nil {
		return err
	}

	syncBin(ctx, lib.logger, lib.root)

	return nil
}

// updateCmd is `update`: the library's, with every flag it has.
type updateCmd struct {
	Interactive   bool `help:"Select the packages with the fuzzy finder."                         name:"i"  short:"i"`
	SelectVersion bool `help:"Select the version with the fuzzy finder; --limit bounds the list." short:"s"`
	OnlyRegistry  bool `help:"Update only registries."                                            short:"r"`
	OnlyPackage   bool `help:"Update only packages."                                              short:"p"`
	//revive:disable-next-line:struct-tag // kong substitutes ${limit} from its Vars before it parses the int.
	Limit       int      `default:"${limit}"                                     help:"The maximum number of versions listed; non-positive is no limit." short:"l"`
	Tags        string   `help:"Update only the packages having these tags."     short:"t"`
	ExcludeTags string   `help:"Update only the packages not having these tags."`
	Packages    []string `arg:""                                                 help:"Command names, each with an optional @version."                   optional:""`
}

// Run updates through the library, then mirrors what it exposed.
func (c *updateCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()
	param.Insert = c.Interactive
	param.SelectVersion = c.SelectVersion
	param.OnlyPackage = c.OnlyPackage
	param.OnlyRegistry = c.OnlyRegistry
	param.Limit = c.Limit
	param.Tags = parseTags(c.Tags)
	param.ExcludedTags = parseTags(c.ExcludeTags)
	param.Args = c.Packages

	ctrl, err := controller.InitializeUpdateCommandController(ctx, lib.logger, param, http.DefaultClient, lib.runtime)
	if err != nil {
		return fmt.Errorf("initialize the update controller: %w", err)
	}

	if err := ctrl.Update(ctx, lib.logger, param); err != nil {
		return err
	}

	syncBin(ctx, lib.logger, lib.root)

	return nil
}

// updateChecksumCmd is `update-checksum`, without the deprecated --deep.
type updateChecksumCmd struct {
	All   bool `help:"Update the global configuration's checksums too." short:"a"`
	Prune bool `help:"Remove unused checksums."`
}

// Run rewrites the checksums through the library.
func (c *updateChecksumCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()
	param.All = c.All
	param.Prune = c.Prune

	ctrl, err := controller.InitializeUpdateChecksumCommandController(
		ctx,
		lib.logger,
		param,
		http.DefaultClient,
		lib.runtime,
	)
	if err != nil {
		return fmt.Errorf("initialize the update-checksum controller: %w", err)
	}

	return ctrl.UpdateChecksum(ctx, lib.logger, param)
}

// generateCmd is `generate`: the library's, with every flag it has.
type generateCmd struct {
	File          string `help:"The file listing the packages; \"-\" reads the standard input."     name:"f"  placeholder:"PATH" short:"f"`
	Insert        bool   `help:"Insert the packages into the configuration file."                   name:"i"  short:"i"`
	Pin           bool   `help:"Pin the version."`
	Global        bool   `help:"Insert the packages into the global configuration file."            name:"g"  short:"g"`
	Detail        bool   `help:"Output additional fields, the description and the link."            short:"d"`
	OutputFile    string `help:"The file to insert into."                                           name:"o"  placeholder:"PATH" short:"o"`
	SelectVersion bool   `help:"Select the version with the fuzzy finder; --limit bounds the list." short:"s"`
	//revive:disable-next-line:struct-tag // kong substitutes ${limit} from its Vars before it parses the int.
	Limit    int      `default:"${limit}" help:"The maximum number of versions listed; non-positive is no limit." short:"l"`
	Packages []string `arg:""             help:"Packages, as <registry name>,<package name>."                     optional:""`
}

// Run generates through the library.
func (c *generateCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()
	param.File = c.File
	param.Insert = c.Insert
	param.Pin = c.Pin
	param.Global = c.Global
	param.Detail = c.Detail
	param.Dest = c.OutputFile
	param.SelectVersion = c.SelectVersion
	param.Limit = c.Limit

	ctrl, err := controller.InitializeGenerateCommandController(ctx, lib.logger, param, http.DefaultClient, lib.runtime)
	if err != nil {
		return fmt.Errorf("initialize the generate controller: %w", err)
	}

	return ctrl.Generate(ctx, lib.logger, param, c.Packages...)
}

// whichCmd is `which`: the path the library resolves for a command name, or
// its package's version.
type whichCmd struct {
	ShowVersion bool   `help:"Output the package's version instead of the path." name:"show-version"`
	Command     string `arg:""                                                   help:"The command name."`
}

// Run prints the path, or the version, the library resolves.
func (c *whichCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()
	param.ShowVersion = c.ShowVersion

	found, err := which(ctx, lib, param, c.Command)
	if err != nil {
		return err
	}

	if !c.ShowVersion {
		fmt.Fprintln(lib.stdout, found.ExePath)

		return nil
	}

	if found.Package == nil {
		return fmt.Errorf("%w: %s", errNotManaged, c.Command)
	}

	fmt.Fprintln(lib.stdout, found.Package.Package.Version)

	return nil
}

// policyCmd is `policy`, with allow and deny: the gate's own keepers.
type policyCmd struct {
	Allow policyAllowCmd `cmd:"" help:"Allow a policy file."`
	Deny  policyDenyCmd  `cmd:"" help:"Deny a policy file."`
}

// policyAllowCmd is `policy allow`.
type policyAllowCmd struct {
	PolicyPath string `arg:"" help:"The policy file." optional:"" placeholder:"PATH"`
}

// Run records the policy as allowed.
func (c *policyAllowCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()

	return controller.InitializeAllowPolicyCommandController(ctx, param).
		Allow(lib.logger, param, c.PolicyPath)
}

// policyDenyCmd is `policy deny`.
type policyDenyCmd struct {
	PolicyPath string `arg:"" help:"The policy file." optional:"" placeholder:"PATH"`
}

// Run removes the policy's record.
func (c *policyDenyCmd) Run(ctx context.Context, lib *library) error {
	param := lib.param()

	return controller.InitializeDenyPolicyCommandController(ctx, param).
		Deny(lib.logger, param, c.PolicyPath)
}

// rootDirCmd is `root-dir`: the library's root, which the binary derives from
// its own, so what it prints is never the shell's AQUA_ROOT_DIR.
type rootDirCmd struct{}

// Run prints the library's root.
func (*rootDirCmd) Run(lib *library) error {
	fmt.Fprintln(lib.stdout, lib.root.AquaRoot())

	return nil
}

// versionCmd is `version`, the --version flag's twin for a script that passes
// a command.
type versionCmd struct{}

// Run prints the version.
func (*versionCmd) Run(lib *library) error {
	fmt.Fprintln(lib.stdout, lib.version)

	return nil
}

// parseTags is the library's reading of a --tags value: comma-separated,
// trimmed, the empty ones dropped.
func parseTags(value string) map[string]struct{} {
	tags := map[string]struct{}{}

	for tag := range strings.SplitSeq(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags[tag] = struct{}{}
		}
	}

	return tags
}
