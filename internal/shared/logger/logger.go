package logger

import (
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

// Init sets up a pretty, colored slog handler as the global default.
// Colors are disabled automatically when stderr is not a TTY (CI, Lambda).
func Init() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:   slog.LevelDebug,
		NoColor: !isatty.IsTerminal(os.Stderr.Fd()),
	})))
}
