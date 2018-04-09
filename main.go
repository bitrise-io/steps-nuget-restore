package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-tools/go-steputils/cache"
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
	log.Infof("Configs:")

	log.Printf("- XamarinSolution: %s", configs.XamarinSolution)
	log.Printf("- NugetVersion: %s", configs.NugetVersion)
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
			log.Warnf("Failed to close (%s)", targetPath)
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
			log.Warnf("failed to close (%s) body", downloadURL)
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
		log.Errorf("Issue with input: %s", err)
		os.Exit(1)
	}

	nugetPth := "/Library/Frameworks/Mono.framework/Versions/Current/bin/nuget"
	nugetRestoreCmdArgs := []string{nugetPth}

	if configs.NugetVersion != "" {
		fmt.Println()
		log.Infof("Downloading Nuget %s version...", configs.NugetVersion)
		tmpDir, err := pathutil.NormalizedOSTempDirPath("__nuget__")
		if err != nil {
			log.Errorf("Failed to create tmp dir, error: %s", err)
			os.Exit(1)
		}

		downloadPth := filepath.Join(tmpDir, "nuget.exe")

		// https://dist.nuget.org/win-x86-commandline/v3.3.0/nuget.exe
		var nugetURL string
		if configs.NugetVersion == "latest" {
			nugetURL = "https://dist.nuget.org/win-x86-commandline/latest/nuget.exe"
		} else {
			nugetURL = fmt.Sprintf("https://dist.nuget.org/win-x86-commandline/v%s/nuget.exe", configs.NugetVersion)
		}

		log.Printf("Download URL: %s", nugetURL)

		if err := DownloadFile(nugetURL, downloadPth); err != nil {
			log.Warnf("Download failed, error: %s", err)

			// https://dist.nuget.org/win-x86-commandline/v3.4.4/NuGet.exe
			nugetURL = fmt.Sprintf("https://dist.nuget.org/win-x86-commandline/v%s/NuGet.exe", configs.NugetVersion)

			log.Printf("Retry download URl: %s", nugetURL)

			if err := DownloadFile(nugetURL, downloadPth); err != nil {
				log.Errorf("Failed to download nuget, error: %s", err)
				os.Exit(1)
			}
		}

		nugetRestoreCmdArgs = []string{constants.MonoPath, downloadPth}
	}

	fmt.Println()
	log.Infof("Restoring Nuget packages...")

	nugetRestoreCmdArgs = append(nugetRestoreCmdArgs, "restore", configs.XamarinSolution)

	if err := retry.Times(1).Try(func(attempt uint) error {
		if attempt > 0 {
			log.Warnf("Attempt %d failed, retrying...", attempt)
		}

		log.Donef("$ %s", command.PrintableCommandArgs(false, nugetRestoreCmdArgs))

		cmd, err := command.NewFromSlice(nugetRestoreCmdArgs)
		if err != nil {
			log.Errorf("Failed to create Nuget command, error: %s", err)
			os.Exit(1)
		}

		cmd.SetStdout(os.Stdout)
		cmd.SetStderr(os.Stderr)

		if err := cmd.Run(); err != nil {
			log.Errorf("Restore failed, error: %s", err)
			return err
		}
		return nil
	}); err != nil {
		log.Errorf("Nuget restore failed, error: %s", err)
		os.Exit(1)
	}

	// Collecting caches
	fmt.Println()
	log.Infof("Collecting NuGet cache...")

	nugetCache := cache.New()

	xamarinHomeCache := filepath.Join(pathutil.UserHomeDir(), ".local", "share", "Xamarin")

	if exists, err := pathutil.IsPathExists(xamarinHomeCache); err != nil {
		log.Warnf("Failed to determine if path (%s) exists, error: %s", xamarinHomeCache, err)
	} else if exists {
		nugetCache.IncludePath(xamarinHomeCache)
	}

	absProjectRoot, err := filepath.Abs(".")
	if err != nil {
		log.Warnf("Cache collection skipped: failed to determine project root path.")
	} else {
		err := filepath.Walk(absProjectRoot, func(path string, f os.FileInfo, err error) error {
			if f.IsDir() {
				if f.Name() == "packages" {
					nugetCache.IncludePath(path)
					return io.EOF
				}
			}
			return nil
		})
		if err == io.EOF {
			err = nil
		}
		if err != nil {
			log.Warnf("Cache collection skipped: failed to determine cache paths.")
		} else {
			if err := nugetCache.Commit(); err != nil {
				log.Warnf("Cache collection skipped: failed to commit cache paths.")
			}
		}
	}
}
