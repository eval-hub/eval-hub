package sql_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var (
	timeout = 2 * time.Minute
)

func getPostgresUser() (string, error) {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		return "", fmt.Errorf("USER/LOGNAME is not set")
	}
	return user, nil
}

func getPostgresURL(databaseName string) (string, error) {
	if dbURL := os.Getenv("POSTGRES_URL"); dbURL != "" {
		return dbURL, nil
	}
	user, err := getPostgresUser()
	if err != nil {
		return "", err
	}
	// postgres://user@localhost:5432/eval_hub
	return fmt.Sprintf("postgres://%s@localhost:5432/%s", user, databaseName), nil
}

func runMakeCommand(t *testing.T, databaseName string, user string, args ...string) error {
	cmd, cancel, err := getMakeCommandWithTimeout(databaseName, user, timeout, args...)
	if err != nil {
		t.Fatalf("Failed to get make command: %v", err)
	}
	defer cancel()
	return cmd.Run()
}

func startPostgres(t *testing.T, databaseName string, user string, image bool) error {
	if image {
		_ = runMakeCommand(t, databaseName, user, "stop-postgres-container")
		_ = runMakeCommand(t, databaseName, user, "delete-postgres-container")
		err := runMakeCommand(t, databaseName, user, "start-postgres-container")
		if err != nil {
			t.Fatalf("Failed to start postgres: %v", err)
		}
	} else {
		t.Logf("Installing postgres GOOS=%s GOARCH=%s", runtime.GOOS, runtime.GOARCH)
		err := runMakeCommand(t, databaseName, user, "install-postgres")
		if err != nil {
			// skip the rest of the test if we can not install postgres
			return fmt.Errorf("failed to install postgres: %w", err)
		}
		err = runMakeCommand(t, databaseName, user, "start-postgres")
		if err != nil {
			return fmt.Errorf("failed to start postgres: %w", err)
		}
		// HACK to wait for the startup
		time.Sleep(10 * time.Second)

		_ = runMakeCommand(t, databaseName, user, "create-database")
		_ = runMakeCommand(t, databaseName, user, "create-user")
		err = runMakeCommand(t, databaseName, user, "grant-permissions")
		if err != nil {
			return fmt.Errorf("failed to grant permissions: %w", err)
		}
	}
	return nil
}

func stopPostgres(t *testing.T, databaseName string, user string, image bool) {
	if image {
		err := runMakeCommand(t, databaseName, user, "stop-postgres-container")
		if err != nil {
			t.Fatalf("Failed to stop postgres: %v", err)
		}
	} else {
		err := runMakeCommand(t, databaseName, user, "stop-postgres")
		if err != nil {
			t.Fatalf("Failed to stop postgres: %v", err)
		}
	}
}

func findDir(dirName string, dirs ...string) (string, error) {
	var found []string
	for _, dir := range dirs {
		name, err := filepath.Abs(filepath.Join(dir, dirName))
		if err != nil {
			return "", fmt.Errorf("Failed to get absolute path for %s: %v", filepath.Join(dir, dirName), err)
		}
		if info, err := os.Stat(name); err == nil {
			if info.IsDir() {
				return name, nil
			}
		}
		found = append(found, name)
	}
	return "", fmt.Errorf("Failed to find directory %s in %v", dirName, found)
}

func getDirForMakefile() (string, error) {
	// set the directory to the tests/postgres directory
	return findDir(filepath.Join("tests", "postgres"), ".", "../..", "../../../..")
}

func getMakeCommandWithTimeout(databaseName string, user string, cmdTimeout time.Duration, args ...string) (*exec.Cmd, context.CancelFunc, error) {
	dir, err := getDirForMakefile()
	if err != nil {
		return nil, nil, err
	}
	if cmdTimeout == 0 {
		cmdTimeout = timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)

	cmd := exec.CommandContext(ctx, "make", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "POSTGRES_DATABASE_NAME="+databaseName, "POSTGRES_USER="+user)

	return cmd, cancel, nil
}
