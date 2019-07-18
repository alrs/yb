package buildpacks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johnewart/archiver"
	. "github.com/yourbase/yb/plumbing"
	. "github.com/yourbase/yb/types"
)

//https://archive.apache.org/dist/maven/maven-3/3.3.3/binaries/apache-maven-3.3.3-bin.tar.gz
var MAVEN_DIST_MIRROR = "https://archive.apache.org/dist/maven/"

type MavenBuildTool struct {
	BuildTool
	version string
	spec    BuildToolSpec
}

func NewMavenBuildTool(toolSpec BuildToolSpec) MavenBuildTool {
	tool := MavenBuildTool{
		version: toolSpec.Version,
		spec:    toolSpec,
	}

	return tool
}

func (bt MavenBuildTool) ArchiveFile() string {
	return fmt.Sprintf("apache-maven-%s-bin.tar.gz", bt.Version())
}

func (bt MavenBuildTool) DownloadUrl() string {
	return fmt.Sprintf(
		"%s/maven-%s/%s/binaries/%s",
		MAVEN_DIST_MIRROR,
		bt.MajorVersion(),
		bt.Version(),
		bt.ArchiveFile(),
	)
}

func (bt MavenBuildTool) MajorVersion() string {
	parts := strings.Split(bt.version, ".")
	return parts[0]
}

func (bt MavenBuildTool) Version() string {
	return bt.version
}

func (bt MavenBuildTool) InstallDir() string {
	return filepath.Join(ToolsDir(), "maven")
}

func (bt MavenBuildTool) MavenDir() string {
	return filepath.Join(bt.InstallDir(), fmt.Sprintf("apache-maven-%s", bt.Version()))
}

func (bt MavenBuildTool) Setup() error {
	mavenDir := bt.MavenDir()
	cmdPath := fmt.Sprintf("%s/bin", mavenDir)
	currentPath := os.Getenv("PATH")
	newPath := fmt.Sprintf("%s:%s", cmdPath, currentPath)
	fmt.Printf("Setting PATH to %s\n", newPath)
	os.Setenv("PATH", newPath)

	return nil
}

// TODO, generalize downloader
func (bt MavenBuildTool) Install() error {
	mavenDir := bt.MavenDir()

	if _, err := os.Stat(mavenDir); err == nil {
		fmt.Printf("Maven v%s located in %s!\n", bt.Version(), mavenDir)
	} else {
		fmt.Printf("Will install Maven v%s into %s\n", bt.Version(), bt.InstallDir())
		downloadUrl := bt.DownloadUrl()

		fmt.Printf("Downloading Maven from URL %s...\n", downloadUrl)
		localFile, err := DownloadFileWithCache(downloadUrl)
		if err != nil {
			fmt.Printf("Unable to download: %v\n", err)
			return err
		}
		err = archiver.Unarchive(localFile, bt.InstallDir())
		if err != nil {
			fmt.Printf("Unable to decompress: %v\n", err)
			return err
		}

	}

	return nil
}