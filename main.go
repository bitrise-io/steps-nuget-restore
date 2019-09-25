package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-xamarin/constants"
	"github.com/bitrise-tools/go-steputils/cache"
)

// ConfigsModel ...
type ConfigsModel struct {
	XamarinSolution string `env:"xamarin_solution,file"`
	NuGetVersion    string `env:"nuget_version"`
	CacheLevel      string `env:"cache_level,opt[local,global,all,none]"`
}

func fail(format string, v ...interface{}) {
	log.Errorf(format, v...)
	os.Exit(1)
}

func (configs ConfigsModel) print() {
	log.Infof("Configs:")

	log.Printf("- XamarinSolution: %s", configs.XamarinSolution)
	log.Printf("- NuGetVersion: %s", configs.NuGetVersion)
}

const (
	cacheInputNone   = "none"
	cacheInputlocal  = "local"
	cacheInputGlobal = "global"
	cacheInputAll    = "all"

	cacheEnvGlobal = "NUGET_PACKAGES"
	cacheEnvHTTP   = "NUGET_HTTP_CACHE_PATH"
)

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
		return fmt.Errorf("request failed, status code: %d", resp.StatusCode)
	}

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to download from (%s), error: %s", downloadURL, err)
	}

	return nil
}

// downloadNuGet downloads NuGet with the given version.
func downloadNuGet(version string) (string, error) {
	fmt.Println()
	log.Infof("Downloading NuGet %s version...", version)
	tmpDir, err := pathutil.NormalizedOSTempDirPath("__nuget__")
	if err != nil {
		return "", fmt.Errorf("failed to create tmp dir, error: %s", err)
	}

	downloadPth := filepath.Join(tmpDir, "nuget.exe")

	// https://dist.nuget.org/win-x86-commandline/latest/nuget.exe or
	// https://dist.nuget.org/win-x86-commandline/v3.3.0/nuget.exe

	if version != "latest" {
		version = `v` + version
	}
	nuGetURL := fmt.Sprintf("https://dist.nuget.org/win-x86-commandline/%s/nuget.exe", version)

	log.Printf("Download URL: %s", nuGetURL)
	return downloadPth, retry.Times(1).Wait(time.Second).Try(func(attempt uint) error {
		if attempt > 0 {
			log.Warnf("Retrying...")
		}
		if err := DownloadFile(nuGetURL, downloadPth); err != nil {
			log.Errorf("Failed to download NuGet, error: %s", err)
			return err
		}
		return nil
	})
}

// runRestoreCommand runs the restore command with the given args.
func runRestoreCommand(nuGetRestoreCmdArgs []string) error {
	return retry.Times(1).Try(func(attempt uint) error {
		if attempt > 0 {
			log.Warnf("Attempt %d failed, retrying...", attempt)
		}

		log.Donef("$ %s", command.PrintableCommandArgs(false, nuGetRestoreCmdArgs))

		cmd, err := command.NewFromSlice(nuGetRestoreCmdArgs)
		if err != nil {
			fail("Failed to create NuGet command, error: %s", err)
		}

		cmd.SetStdout(os.Stdout)
		cmd.SetStderr(os.Stderr)

		if err := cmd.Run(); err != nil {
			log.Errorf("Restore failed, error: %s", err)
			return err
		}
		return nil
	})
}

// collectCaches collects the caches based on the config.
// For more information about caches please read: https://docs.microsoft.com/en-us/nuget/consume-packages/managing-the-global-packages-and-cache-folders
func collectCaches(cacheLevel string, basePth string) error {
	nuGetCache := cache.New()
	switch cacheLevel {
	case cacheInputNone:
		return nil
	case cacheInputlocal:
		localCaches, err := collectLocalCaches(basePth)
		if err != nil {
			log.Warnf("Error occurred while getting local cache. Error: %s", err)
		}
		for _, lcItem := range localCaches {
			nuGetCache.IncludePath(lcItem)
		}
	case cacheInputGlobal:
		nuGetCache.IncludePath(collectGlobalCaches())
	case cacheInputAll:
		localCaches, err := collectLocalCaches(basePth)
		if err != nil {
			log.Warnf("Error occurred while getting all cache. Error: %s", err)
		}
		for _, lcItem := range localCaches {
			nuGetCache.IncludePath(lcItem)
		}
		nuGetCache.IncludePath(collectGlobalCaches())
	}
	return nuGetCache.Commit()
}

// collectHTTPCaches collects the HTTP cache.
func collectHTTPCaches() string {
	httpCachePth := HTTPCachePath()
	if exists, err := pathutil.IsPathExists(httpCachePth); err != nil {
		log.Warnf("Failed to determine if path (%s) exists, error: %s", httpCachePth, err)
	} else if exists {
		return httpCachePth
	}
	return ""
}

// HTTPCachePath gets the path for the HTTP cache.
func HTTPCachePath() string {
	if pth := os.Getenv(cacheEnvHTTP); pth != "" {
		return pth
	}
	return filepath.Join(pathutil.UserHomeDir(), ".local", "share", "NuGet", "v3-cache")
}

// collectGlobalCaches collects the global package caches.
func collectGlobalCaches() string {
	if pth := os.Getenv(cacheEnvGlobal); pth != "" {
		return pth
	}
	return filepath.Join(pathutil.UserHomeDir(), ".nuget", "packages")
}

// collectLocalCaches collects the local caches.
func collectLocalCaches(basePth string) ([]string, error) {
	var caches []string
	absProjectRoot, err := filepath.Abs(basePth)
	if err != nil {
		return []string{}, fmt.Errorf("cache collection skipped: failed to determine project root path")
	}
	if err := filepath.Walk(absProjectRoot, func(path string, f os.FileInfo, err error) error {
		if f.IsDir() {
			if f.Name() == "packages" {
				caches = append(caches, path)
				return io.EOF
			}
		}
		return nil
	}); err != nil && err != io.EOF {
		return []string{}, fmt.Errorf("cache collection skipped: failed to determine cache paths. Error: %s", err)
	}

	return caches, nil
}

func main() {
	var configs ConfigsModel
	if err := stepconf.Parse(&configs); err != nil {
		fail("Issue with input: %s", err)
	}

	fmt.Println()
	configs.print()

	nuGetPth := "/Library/Frameworks/Mono.framework/Versions/Current/bin/nuget"
	nuGetRestoreCmdArgs := []string{nuGetPth}
	if configs.NuGetVersion != "" {
		downloadPth, err := downloadNuGet(configs.NuGetVersion)
		if err != nil {
			fail("%s", err)
		}
		nuGetRestoreCmdArgs = []string{constants.MonoPath, downloadPth}
	}

	fmt.Println()
	log.Infof("Restoring NuGet packages...")

	nuGetRestoreCmdArgs = append(nuGetRestoreCmdArgs, "restore", configs.XamarinSolution)
	if err := runRestoreCommand(nuGetRestoreCmdArgs); err != nil {
		fail("NuGet restore failed, error: %s", err)
	}

	// Collecting caches
	fmt.Println()
	log.Infof("Collecting NuGet cache...")
	if err := collectCaches(configs.CacheLevel, path.Dir(configs.XamarinSolution)); err != nil {
		log.Warnf("Cache collection skipped: failed to commit cache paths.")
	}
}
