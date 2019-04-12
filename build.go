package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/johnewart/subcommands"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const TIME_FORMAT = "15:04:05 MST"

type buildCmd struct {
	ExecPrefix  string
	NoContainer bool
}

func (*buildCmd) Name() string     { return "build" }
func (*buildCmd) Synopsis() string { return "Build the workspace" }
func (*buildCmd) Usage() string {
	return `build`
}

func (b *buildCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&b.NoContainer, "no-container", false, "Bypass container even if specified")
	f.StringVar(&b.ExecPrefix, "exec-prefix", "", "Add a prefix to all executed commands (useful for timing or wrapping things)")
}

func (b *buildCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	startTime := time.Now()

	fmt.Printf("Build started at %s\n", startTime.Format(TIME_FORMAT))
	realStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	outputs := io.MultiWriter(realStdout)
	var buf bytes.Buffer
	uploadBuildLogs := false

	if v, err := GetConfigValue("user", "upload_build_logs"); err == nil {
		if v == "true" {
			uploadBuildLogs = true
			outputs = io.MultiWriter(realStdout, &buf)
		}
	}

	// copy the output in a separate goroutine so printing can't block indefinitely
	go func() {
		for {
			io.Copy(outputs, r)
		}
	}()

	defer w.Close()
	defer r.Close()

	workspace := LoadWorkspace()
	buildTarget := "default"
	var targetPackage string

	if PathExists(MANIFEST_FILE) {
		currentPath, _ := filepath.Abs(".")
		_, file := filepath.Split(currentPath)
		targetPackage = file
	} else {
		targetPackage = workspace.Target
	}

	if len(f.Args()) > 0 {
		buildTarget = f.Args()[0]
	}

	targetDir := workspace.PackagePath(targetPackage)

	fmt.Printf("Building target package %s in %s...\n", targetPackage, targetDir)
	instructions, err := workspace.LoadPackageManifest(targetPackage)

	if err != nil {
		fmt.Printf("Unable to load package manifest for %s: %v\n", targetPackage, err)
		return subcommands.ExitFailure
	}

	fmt.Printf("Working in %s...\n", targetDir)

	var target BuildPhase
	sandboxed := instructions.Sandbox || target.Sandbox

	if len(instructions.BuildTargets) == 0 {
		target = instructions.Build
		if len(target.Commands) == 0 {
			fmt.Printf("Default build command has no steps and no targets described\n")
		}
	} else {
		ok := false
		if target, ok = instructions.BuildTargets[buildTarget]; !ok {
			targets := make([]string, 0, len(instructions.BuildTargets))
			for t := range instructions.BuildTargets {
				targets = append(targets, t)
			}

			fmt.Printf("Build target %s specified but it doesn't exist!\n")
			fmt.Printf("Valid build targets: %s\n", strings.Join(targets, ", "))
		}
	}

	// Ensure build deps are :+1:
	workspace.SetupBuildDependencies(*instructions)

	// Set any environment variables as the last thing (override things we do in case people really want to do this)
	// XXX: Should we though?
	for _, envString := range target.Environment {
		parts := strings.Split(envString, "=")
		key := parts[0]
		value := parts[1]
		value = strings.Replace(value, "{PKGDIR}", targetDir, -1)
		fmt.Printf("Setting %s = %s\n", key, value)
		os.Setenv(key, value)
	}

	config := BuildConfiguration{
		Target:     target,
		Sandboxed:  sandboxed,
		ExecPrefix: b.ExecPrefix,
		TargetDir:  targetDir,
	}

	var stepTimes []CommandTimer
	var buildError error

	if target.Container.Image != "" && !b.NoContainer {
		fmt.Println("Executing build steps in container")
		containerOpts := BuildContainerOpts{
			ContainerOpts: target.Container,
			PackageName:   targetPackage,
			Workspace:     workspace,
		}

		stepTimes, buildError = RunCommandsInContainer(config, containerOpts)
	} else {
		// Do the commands
		fmt.Println("Executing build steps")
		stepTimes, buildError = RunCommands(config)
	}

	endTime := time.Now()
	buildTime := endTime.Sub(startTime)

	fmt.Printf("\nBuild finished at %s, taking %s\n", endTime.Format(TIME_FORMAT), buildTime)
	fmt.Println()
	fmt.Printf("%15s%15s%15s   %s\n", "Start", "End", "Elapsed", "Command")
	for _, step := range stepTimes {
		elapsed := step.EndTime.Sub(step.StartTime)
		fmt.Printf("%15s%15s%15s   %s\n",
			step.StartTime.Format(TIME_FORMAT),
			step.EndTime.Format(TIME_FORMAT),
			elapsed,
			step.Command)
	}
	fmt.Printf("\n%15s%15s%15s   %s\n", "", "", buildTime, "TOTAL")

	if buildError != nil {
		fmt.Println("\n\n -- BUILD FAILED -- ")
	} else {
		fmt.Println("\n\n -- BUILD SUCCEEDED -- ")
	}

	// Reset stdout
	os.Stdout = realStdout

	if uploadBuildLogs {
		UploadBuildLogsToAPI(&buf)
	}

	if buildError != nil {
		return subcommands.ExitFailure
	}

	// No errors, :+1:
	return subcommands.ExitSuccess

}

type BuildConfiguration struct {
	Target     BuildPhase
	TargetDir  string
	Sandboxed  bool
	ExecPrefix string
}

func RunCommandsInContainer(config BuildConfiguration, containerOpts BuildContainerOpts) ([]CommandTimer, error) {
	stepTimes := make([]CommandTimer, 0)
	target := config.Target

	// Perform build inside a container
	image := target.Container.Image
	fmt.Printf("Invoking build in a container: %s\n", image)

	var buildContainer BuildContainer

	existing, err := FindContainer(containerOpts)

	if err != nil {
		fmt.Printf("Failed trying to find container: %v\n", err)
		return stepTimes, err
	}

	if existing != nil {
		fmt.Printf("Found existing container %s, removing...\n", existing.Id)
		if err = RemoveContainerById(existing.Id); err != nil {
			fmt.Printf("Unable to remove existing container: %v\n", err)
			return stepTimes, err
		}
	}

	buildContainer, err = NewContainer(containerOpts)
	if err != nil {
		fmt.Printf("Error creating build container: %v\n", err)
		return stepTimes, err
	}

	if err := buildContainer.Start(); err != nil {
		fmt.Printf("Unable to start container %s: %v", buildContainer.Id, err)
		return stepTimes, err
	}

	fmt.Printf("Building in container: %s\n", buildContainer.Id)

	for _, cmdString := range target.Commands {
		stepStartTime := time.Now()
		if len(config.ExecPrefix) > 0 {
			cmdString = fmt.Sprintf("%s %s", config.ExecPrefix, cmdString)
		}

		fmt.Printf("Running %s in the container\n", cmdString)

		if err := buildContainer.ExecToStdout(cmdString); err != nil {
			fmt.Printf("Failed to run %s: %v", cmdString, err)
			return stepTimes, fmt.Errorf("Aborting build, unable to run %s: %v\n")
		}

		stepEndTime := time.Now()
		stepTotalTime := stepEndTime.Sub(stepStartTime)

		fmt.Printf("Completed '%s' in %s\n", cmdString, stepTotalTime)

		cmdTimer := CommandTimer{
			Command:   cmdString,
			StartTime: stepStartTime,
			EndTime:   stepEndTime,
		}

		stepTimes = append(stepTimes, cmdTimer)
		// Make sure our goroutine gets this from stdout
		// TODO: There must be a better way...
		time.Sleep(10 * time.Millisecond)

	}

	return stepTimes, nil
}

func RunCommands(config BuildConfiguration) ([]CommandTimer, error) {

	stepTimes := make([]CommandTimer, 0)

	target := config.Target
	sandboxed := config.Sandboxed
	targetDir := config.TargetDir

	for _, cmdString := range target.Commands {
		stepStartTime := time.Now()
		if len(config.ExecPrefix) > 0 {
			cmdString = fmt.Sprintf("%s %s", config.ExecPrefix, cmdString)
		}

		if strings.HasPrefix(cmdString, "cd ") {
			parts := strings.SplitN(cmdString, " ", 2)
			dir := filepath.Join(targetDir, parts[1])
			//fmt.Printf("Chdir to %s\n", dir)
			//os.Chdir(dir)
			targetDir = dir
		} else {
			if target.Root != "" {
				fmt.Printf("Build root is %s\n", target.Root)
				targetDir = filepath.Join(targetDir, target.Root)
			}

			if sandboxed {
				fmt.Println("Running build in a sandbox!")
				if err := ExecInSandbox(cmdString, targetDir); err != nil {
					fmt.Printf("Failed to run %s: %v", cmdString, err)
					return stepTimes, err
				}
			} else {
				if err := ExecToStdout(cmdString, targetDir); err != nil {
					fmt.Printf("Failed to run %s: %v", cmdString, err)
					return stepTimes, err
				}
			}
		}

		stepEndTime := time.Now()
		stepTotalTime := stepEndTime.Sub(stepStartTime)

		fmt.Printf("Completed '%s' in %s\n", cmdString, stepTotalTime)

		cmdTimer := CommandTimer{
			Command:   cmdString,
			StartTime: stepStartTime,
			EndTime:   stepEndTime,
		}

		stepTimes = append(stepTimes, cmdTimer)
		// Make sure our goroutine gets this from stdout
		// TODO: There must be a better way...
		time.Sleep(10 * time.Millisecond)
	}

	return stepTimes, nil
}

func UploadBuildLogsToAPI(buf *bytes.Buffer) {
	fmt.Println("Uploading build logs...")
	buildLog := BuildLog{
		Contents: buf.String(),
	}
	jsonData, _ := json.Marshal(buildLog)
	resp, err := postJsonToApi("/buildlogs", jsonData)

	if err != nil {
		fmt.Printf("Couldn't upload logs: %v\n", err)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Couldn't read response body: %s\n", err)
		return
	}

	if resp.StatusCode != 200 {
		fmt.Printf("Status code uploading log: %d\n", resp.StatusCode)
		fmt.Println(string(body))
		return
	} else {
		var buildLog BuildLog
		err = json.Unmarshal(body, &buildLog)
		if err != nil {
			fmt.Printf("Failed to parse response: %v\n", err)
			return
		}

		logViewPath := fmt.Sprintf("/buildlogs/%s", buildLog.UUID)
		fmt.Printf("View your build log here: %s\n", ManagementUrl(logViewPath))
	}

}

type BuildLog struct {
	Contents string `json:"contents"`
	UUID     string `json:"uuid"`
}

type CommandTimer struct {
	Command   string
	StartTime time.Time
	EndTime   time.Time
}
