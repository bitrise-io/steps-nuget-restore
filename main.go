package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/cmdex"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-tools/go-xamarin/constants"
)

// ConfigsModel ...
type ConfigsModel struct {
	XamarinSolution string
	NugetVersion    string
}

func createConfigsModelFromEnvs() ConfigsModel {
	return ConfigsModel{
		XamarinSolution: os.Getenv("xamarin_solution"),
		NugetVersion:    os.Getenv("nuget_version"),
	}
}

func (configs ConfigsModel) print() {
	log.Info("Configs:")

	log.Detail("- XamarinSolution: %s", configs.XamarinSolution)
	log.Detail("- NugetVersion: %s", configs.NugetVersion)
}

func (configs ConfigsModel) validate() error {
	if configs.XamarinSolution == "" {
		return errors.New("no XamarinSolution parameter specified")
	}
	if exist, err := pathutil.IsPathExists(configs.XamarinSolution); err != nil {
		return fmt.Errorf("failed to check if XamarinSolution exist at: %s, error: %s", configs.XamarinSolution, err)
	} else if !exist {
		return fmt.Errorf("xamarinSolution not exist at: %s", configs.XamarinSolution)
	}

	return nil
}

// DownloadFile ...
func DownloadFile(downloadURL, targetPath string) error {
	outFile, err := os.Create(targetPath)
	defer func() {
		if err := outFile.Close(); err != nil {
			log.Warn("Failed to close (%s)", targetPath)
		}
	}()
	if err != nil {
		return fmt.Errorf("failed to create (%s), error: %s", targetPath, err)
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download from (%s), error: %s", downloadURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn("failed to close (%s) body", downloadURL)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non success status code: %d", resp.StatusCode)
	}

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to download from (%s), error: %s", downloadURL, err)
	}

	return nil
}

func main() {
	configs := createConfigsModelFromEnvs()

	fmt.Println()
	configs.print()

	if err := configs.validate(); err != nil {
		log.Error("Issue with input: %s", err)
		os.Exit(1)
	}

	nugetPth := "/Library/Frameworks/Mono.framework/Versions/Current/bin/nuget"
	nugetRestoreCmdArgs := []string{nugetPth}

	if configs.NugetVersion == "latest" {
		fmt.Println()
		log.Info("Updating Nuget to latest version...")
		// "sudo $nuget update -self"
		cmdArgs := []string{"sudo", nugetPth, "update", "-self"}
		cmd, err := cmdex.NewCommandFromSlice(cmdArgs)
		if err != nil {
			log.Error("Failed to create command from args (%v), error: %s", cmdArgs, err)
			os.Exit(1)
		}

		cmd.SetStdout(os.Stdout)
		cmd.SetStderr(os.Stderr)

		log.Done("$ %s", cmdex.PrintableCommandArgs(false, cmdArgs))

		if err := cmd.Run(); err != nil {
			log.Error("Failed to update nuget, error: %s", err)
			os.Exit(1)
		}
	} else if configs.NugetVersion != "" {
		fmt.Println()
		log.Info("Downloading Nuget %s version...", configs.NugetVersion)
		tmpDir, err := pathutil.NormalizedOSTempDirPath("__nuget__")
		if err != nil {
			log.Error("Failed to create tmp dir, error: %s", err)
			os.Exit(1)
		}

		downloadPth := filepath.Join(tmpDir, "nuget.exe")

		// https://dist.nuget.org/win-x86-commandline/v3.3.0/nuget.exe
		nugetURL := fmt.Sprintf("https://dist.nuget.org/win-x86-commandline/v%s/nuget.exe", configs.NugetVersion)

		log.Detail("Download URL: %s", nugetURL)

		if err := DownloadFile(nugetURL, downloadPth); err != nil {
			log.Warn("Download failed, error: %s", err)

			// https://dist.nuget.org/win-x86-commandline/v3.4.4/NuGet.exe
			nugetURL = fmt.Sprintf("https://dist.nuget.org/win-x86-commandline/v%s/NuGet.exe", configs.NugetVersion)

			log.Detail("Retry download URl: %s", nugetURL)

			if err := DownloadFile(nugetURL, downloadPth); err != nil {
				log.Error("Failed to download nuget, error: %s", err)
				os.Exit(1)
			}
		}

		nugetRestoreCmdArgs = []string{constants.MonoPath, downloadPth}
	}

	fmt.Println()
	log.Info("Restoring Nuget packages...")

	nugetRestoreCmdArgs = append(nugetRestoreCmdArgs, "restore", configs.XamarinSolution)
	log.Done("$ %s", cmdex.PrintableCommandArgs(false, nugetRestoreCmdArgs))

	cmd, err := cmdex.NewCommandFromSlice(nugetRestoreCmdArgs)
	if err != nil {
		log.Error("Failed to create Nuget command, error: %s", err)
		os.Exit(1)
	}

	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)

	if err := retry.Times(1).Try(func(attempt uint) error {
		return cmd.Run()
	}); err != nil {
		log.Error("Nuget restore failed, error: %s", err)
		os.Exit(1)
	}
}
