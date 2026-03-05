package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/constants"
)

// handle ready and termination messages

func GetTerminationFile(conf *config.Config, logger *slog.Logger) string {
	tf := ""
	if (conf != nil) && (conf.Service != nil) {
		tf = strings.TrimSpace(conf.Service.TerminationFile)
		if len(tf) > 0 {
			return tf
		}
	}
	// if the config file fails then we still need to be able to get this
	tf = os.Getenv(constants.EnvVarTerminationFile)
	if tf != "" {
		logger.Info("Termination file set from environment variable", "env", constants.EnvVarTerminationFile, "file", tf)
		return tf
	}
	// this must exist and not be part of the readonly file system
	tf = "/opt/evalhub/work/termination-log"
	logger.Info("Termination file fallback value", "file", tf)
	return tf
}

func writeFile(fname string, message string, fileType string, logger *slog.Logger) error {
	filename := filepath.Clean(fname)
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create the %s file: %s: %w", fileType, filename, err)
	}
	_, err = file.Write([]byte(message))
	if err1 := file.Close(); err1 != nil && err == nil {
		err = err1
	}
	if err != nil {
		logger.Error(fmt.Sprintf("when trying to write %s message", fileType), "file", filename, "message", message, "error", err.Error())
	} else {
		logger.Info(fmt.Sprintf("Set %s message", fileType), "message", message)
	}
	return err
}

func getReadyContents(conf *config.Config) string {
	return fmt.Sprintf("Version: %s\nBuild: %s\nBuildDate: %s\n", conf.Service.Version, conf.Service.Build, conf.Service.BuildDate)
}

func SetReady(conf *config.Config, logger *slog.Logger) error {
	return writeFile(conf.Service.ReadyFile, getReadyContents(conf), "ready", logger)
}

func SetTerminationMessage(terminationFile string, message string, logger *slog.Logger) error {
	return writeFile(terminationFile, message, "termination", logger)
}
