package compose

import (
	"context"
	"fmt"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
)

// Load reads the project's Compose files with compose-go: it merges the
// files in order, resolves include and extends, interpolates variables and
// applies the active profiles.
//
// Inputs: p's Files, EnvFiles, Profiles, WorkingDir and Name.
// Returns: the normalized compose-go project (paths resolved to absolute,
// env_file and label_file merged into environment and labels).
// Notes: variables come from the OS environment first, then the env files
// (or the working directory's .env), so the shell wins, as in Docker
// Compose. Only local files are read; remote includes are not enabled.
func Load(ctx context.Context, p *Project) (*types.Project, error) {
	opts := []cli.ProjectOptionsFn{
		cli.WithName(p.Name),
		cli.WithOsEnv,
		cli.WithEnvFiles(p.EnvFiles...),
		cli.WithDotEnv,
		cli.WithProfiles(p.Profiles),
		cli.WithResolvedPaths(true),
	}
	if p.WorkingDir != "" {
		opts = append([]cli.ProjectOptionsFn{cli.WithWorkingDirectory(p.WorkingDir)}, opts...)
	}
	o, err := cli.NewProjectOptions(p.Files, opts...)
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	proj, err := o.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	return proj, nil
}
