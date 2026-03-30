package common_test

import (
	"regexp"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
)

func TestGUID(t *testing.T) {
	g := common.GUID()
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(g) {
		t.Errorf("GUID() = %q, want lowercase UUID string", g)
	}
}
