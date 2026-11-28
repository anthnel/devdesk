package mcp

import (
	"os"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// The fake docker of fakecli is this test binary started under another name.
func TestMain(m *testing.M) { os.Exit(fakecli.Main(m)) }
