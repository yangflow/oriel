package server

import (
	"os"
	"testing"

	"github.com/yangflow/oriel/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Run(m)) }
