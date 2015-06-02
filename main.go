package main

import (
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/danmarg/outtake/lib"
	"github.com/danmarg/outtake/lib/gmail"
	"os"
	"time"
)

const (
	progressUpdateFreqSecs = 2.0
)

func main() {
	app := cli.NewApp()
	app.Name = "outtake"
	app.Usage = "Export Gmail to Maildir...efficiently!"
	app.Version = "0.0.1"
	app.Author = "dan@af0.net"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "directory",
			Usage: "Maildir to output to.",
		},
		cli.BoolFlag{
			Name:  "full",
			Usage: "Force a full sync",
		},
		cli.StringFlag{
			Name:  "label",
			Usage: "Label to sync",
		},
	}
	app.Action = func(ctx *cli.Context) {
		d := ctx.String("directory")
		if d == "" {
			fmt.Println("Missing --directory flag")
			return
		}
		if s, err := os.Stat(d); err != nil && os.IsNotExist(err) {
			if err := os.MkdirAll(d, 0766); err != nil {
				fmt.Println("Error: ", err)
				return
			}
		} else if err != nil {
			fmt.Println("Error: ", err)
			return
		} else if !s.IsDir() {
			fmt.Printf("Error: %d exists and is not a directory\n", d)
			return
		}
		g, err := gmail.NewGmail(d, ctx.String("label"))
		if err != nil {
			fmt.Println("Error: ", err)
			return
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

	}
	app.Run(os.Args)
}
