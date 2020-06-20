package main

import (
	"fmt"
	"github.com/danmarg/outtake/lib"
	"github.com/danmarg/outtake/lib/gmail"
	"github.com/urfave/cli/v2"
	"os"
	"time"
)

const (
	progressUpdateFreqSecs = 2.0
)

func main() {
	app := &cli.App{
		Name:    "outtake",
		Usage:   "Export Gmail to Maildir...efficiently!",
		Version: "0.0.1",
		Authors: []*cli.Author{&cli.Author{
			Name: "Daniel Margolis", Email: "dan@af0.net"}},
	}
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "directory",
			Usage: "Maildir to output to.",
		},
		&cli.BoolFlag{
			Name:  "full",
			Usage: "Force a full sync",
		},
		&cli.StringFlag{
			Name:  "label",
			Usage: "Label to sync",
		},
		&cli.IntFlag{
			Name:  "buffer",
			Usage: "Download buffer size",
			Value: 128,
		},
		&cli.IntFlag{
			Name:  "parallel",
			Usage: "Max parallel downloads",
			Value: 8,
		},
	}
	app.Action = func(ctx *cli.Context) error {
		d := ctx.String("directory")
		if d == "" {
			return fmt.Errorf("Missing --directory flag")
		}
		if s, err := os.Stat(d); err != nil && os.IsNotExist(err) {
			if err := os.MkdirAll(d, 0766); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !s.IsDir() {
			return fmt.Errorf("Error: %d exists and is not a directory\n", d)
		}
		g, err := gmail.NewGmail(d, ctx.String("label"))
		gmail.MessageBufferSize = ctx.Int("buffer")
		gmail.ConcurrentDownloads = ctx.Int("parallel")
		if err != nil {
			return err
		}
		progress := make(chan lib.Progress)
		go func() {
			l := time.Time{}
			for p := range progress {
				if time.Since(l).Seconds() > progressUpdateFreqSecs {
					l = time.Now()
					fmt.Printf("\r%d / %d   %.2f%%  ", p.Current, p.Total, float32(p.Current)/float32(p.Total)*100)
				}
			}
			fmt.Println()
		}()
		if err := g.Sync(ctx.Bool("full"), progress); err != nil {
			fmt.Println(err)
			os.Exit(-1)
		}
		return nil
	}
	app.Run(os.Args)
}
