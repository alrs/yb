package buildpacks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yourbase/yb/plumbing/log"
	"github.com/yourbase/yb/runtime"
)

const rlangDistMirrorTemplate = "https://cloud.r-project.org/src/base"

type RLangBuildTool struct {
	version string
	spec    BuildToolSpec
}

func NewRLangBuildTool(toolSpec BuildToolSpec) RLangBuildTool {
	tool := RLangBuildTool{
		version: toolSpec.Version,
		spec:    toolSpec,
	}

	return tool
}

func (bt RLangBuildTool) ArchiveFile() string {
	return fmt.Sprintf("R-%s.tar.gz", bt.Version())
}

func (bt RLangBuildTool) DownloadURL(ctx context.Context) (string, error) {
	return fmt.Sprintf(
		"%s/R-%s/%s",
		rlangDistMirrorTemplate,
		bt.MajorVersion(),
		bt.ArchiveFile(),
	), nil
}

func (bt RLangBuildTool) MajorVersion() string {
	parts := strings.Split(bt.version, ".")
	return parts[0]
}

func (bt RLangBuildTool) Version() string {
	return bt.version
}

func (bt RLangBuildTool) Setup(ctx context.Context, rlangDir string) error {
	t := bt.spec.InstallTarget

	t.PrependToPath(ctx, filepath.Join(rlangDir, "bin"))

	return nil
}

func (bt RLangBuildTool) Install(ctx context.Context) (string, error) {
	t := bt.spec.InstallTarget

	installDir := filepath.Join(t.ToolsDir(ctx), "R")
	rlangDir := filepath.Join(installDir, "R-"+bt.Version())

	if t.PathExists(ctx, rlangDir) {
		log.Infof("R v%s located in %s!", bt.Version(), rlangDir)
		return rlangDir, nil
	}
	log.Infof("Will install R v%s into %s", bt.Version(), installDir)
	downloadURL, err := bt.DownloadURL(ctx)
	if err != nil {
		log.Errorf("Unable to generate download URL: %v", err)
		return "", err
	}

	log.Infof("Downloading from URL %s ...", downloadURL)
	localFile, err := t.DownloadFile(ctx, downloadURL)
	if err != nil {
		log.Errorf("Unable to download: %v", err)
		return "", err
	}

	tmpDir := filepath.Join(installDir, "src")
	srcDir := filepath.Join(tmpDir, fmt.Sprintf("R-%s", bt.Version()))

	if !t.PathExists(ctx, srcDir) {
		err = t.Unarchive(ctx, localFile, tmpDir)
		if err != nil {
			log.Errorf("Unable to decompress: %v", err)
			return "", err
		}
	}

	t.MkdirAsNeeded(ctx, rlangDir)
	p := runtime.Process{
		Command:   "./configure --with-x=no --prefix=" + rlangDir,
		Directory: srcDir,
	}
	t.Run(ctx, p)

	// TODO guarantee that we have 'make' installed
	p.Command = "make"
	t.Run(ctx, p)
	p.Command = "make install"
	t.Run(ctx, p)

	return rlangDir, nil
}
