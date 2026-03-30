package sql_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/storage/sql/shared"
)

var (
	timeout = 2 * time.Minute
)

func getPostgresURL(dbName string) (string, string, error) {
	if dbURL := os.Getenv("POSTGRES_URL"); dbURL != "" {
		dbConfig := shared.SQLDatabaseConfig{
			URL: dbURL,
		}
		return dbURL, dbConfig.GetUser(), nil
	}
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		return "", "", fmt.Errorf("USER/LOGNAME is not set")
	}
	// postgres://user@localhost:5432/eval_hub
	return fmt.Sprintf("postgres://%s@localhost:5432/%s", user, dbName), user, nil
}

func startPostgres(t *testing.T, databaseName string, user string) {
	// we need to stop postgres after the test finishes
	t.Cleanup(func() {
		// stopPostgres(t, databaseName, user)
	})

	cmd, cancel, err := getMakeCommand(databaseName, user, "install-postgres", "start-postgres")
	if err != nil {
		t.Fatalf("Failed to get make command: %v", err)
	}
	defer cancel()

	err = cmd.Run()
	if err != nil {
		t.Fatalf("Failed to install and start postgres: %v", err)
	}

	// wait for postgres to start
	time.Sleep(15 * time.Second)

	cmd, cancel, err = getMakeCommand(databaseName, user, "create-user")
	if err != nil {
		t.Fatalf("Failed to get make command: %v", err)
	}
	defer cancel()

	err = cmd.Run()
	if err != nil {
		// not a fatal error for now
		t.Errorf("Failed to create user: %v", err)
	}

	cmd, cancel, err = getMakeCommand(databaseName, user, "create-database")
	if err != nil {
		t.Fatalf("Failed to get make command: %v", err)
	}
	defer cancel()

	err = cmd.Run()
	if err != nil {
		t.Errorf("Failed to create database: %v", err)
	}

	cmd, cancel, err = getMakeCommand(databaseName, user, "grant-permissions")
	if err != nil {
		t.Fatalf("Failed to get make command: %v", err)
	}
	defer cancel()

	err = cmd.Run()
	if err != nil {
		t.Fatalf("Failed to grant permissions: %v", err)
	}
}

func stopPostgres(t *testing.T, databaseName string, user string) {
	cmd, cancel, err := getMakeCommand(databaseName, user, "stop-postgres")
	if err != nil {
		t.Fatalf("Failed to get make command: %v", err)
	}
	defer cancel()

	err = cmd.Run()
	if err != nil {
		t.Fatalf("Failed to stop postgres: %v", err)
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

func getMakeCommand(databaseName string, user string, args ...string) (*exec.Cmd, context.CancelFunc, error) {
	dir, err := getDirForMakefile()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	cmd := exec.CommandContext(ctx, "make", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "POSTGRES_DATABASE_NAME="+databaseName, "POSTGRES_USER="+user)

	return cmd, cancel, nil
}
