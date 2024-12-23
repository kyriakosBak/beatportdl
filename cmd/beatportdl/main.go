package main

import (
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/vbauerster/mpb/v8"
	"io"
	"os"
	"strings"
	"sync"
	"unspok3n/beatportdl/config"
	"unspok3n/beatportdl/internal/beatport"
)

const (
	configFilename = "beatportdl-config.yml"
	cacheFilename  = "beatportdl-credentials.json"
)

type application struct {
	config      *config.AppConfig
	logFile     *os.File
	logWriter   io.Writer
	bp          *beatport.Beatport
	wg          sync.WaitGroup
	downloadSem chan struct{}
	globalSem   chan struct{}
	pbp         *mpb.Progress
	urls        []string
}

func main() {
	cfg, cachePath, err := Setup()
	if err != nil {
		fmt.Println(err.Error())
		Pause()
	}

	flag.Parse()
	inputArgs := flag.Args()

	// Append path if present in args
	if len(inputArgs) > 0 && strings.HasPrefix(inputArgs[0], "-PATH") {
		newPath := inputArgs[0][5:]
		cfg.DownloadsDirectory = newPath 
	}

	app := &application{
		config:      cfg,
		downloadSem: make(chan struct{}, cfg.MaxDownloadWorkers),
		globalSem:   make(chan struct{}, cfg.MaxGlobalWorkers),
		logWriter:   os.Stdout,
	}

	if cfg.WriteErrorLog {
		logFilePath, err := ExecutableDirFilePath("error.log")
		if err != nil {
			fmt.Println(err.Error())
			Pause()
		}
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
		app.logFile = f
		defer f.Close()
	}

	bp := beatport.New(
		cfg.Username,
		cfg.Password,
		cachePath,
		cfg.Proxy,
	)

	if err := bp.LoadCachedTokenPair(); err != nil {
		if err := bp.NewTokenPair(); err != nil {
			app.FatalError("beatport", err)
		}
	}

	app.bp = bp

	for _, arg := range inputArgs {
		if strings.HasPrefix(arg, "-PATH") {
			continue
		}
		if strings.HasSuffix(arg, ".txt") {
			app.parseTextFile(arg)
		} else {
			app.urls = append(app.urls, arg)
		}
	}

	if len(app.urls) == 0 {
		app.mainPrompt()
	}

	app.pbp = mpb.New(mpb.WithAutoRefresh(), mpb.WithOutput(color.Output))
	app.logWriter = app.pbp

	for _, url := range app.urls {
		app.background(func() {
			app.handleUrl(url)
		})
	}

	app.wg.Wait()
	app.pbp.Shutdown()

	app.urls = []string{}
}
