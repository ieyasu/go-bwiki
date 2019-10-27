package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
)

type wikiConfig struct {
	wikiName string
	servAddr string
	devMode  bool
}

var cfg wikiConfig = wikiConfig{wikiName: "BWiki",
	servAddr: "127.0.0.1:8080", devMode: true}

var cfgLinePat *regexp.Regexp = regexp.MustCompile(`^\s*(\w+)\s*=\s*([^\r\n]*)`)
var cfgEmptyPat *regexp.Regexp = regexp.MustCompile(`^\s*(?:#.*)?`)

func readConfig() {
	fin, err := os.Open("wiki.conf")
	confErr(err)
	defer fin.Close()

	scanner := bufio.NewScanner(fin)
	for scanner.Scan() {
		b := scanner.Bytes()
		m := cfgLinePat.FindSubmatch(b)
		if len(m) == 3 {
			m2 := string(m[2])
			switch string(m[1]) {
			case "name":
				cfg.wikiName = m2
			case "serv_addr":
				cfg.servAddr = m2
			case "dev_mode":
				cfg.devMode = (m2 == "true" || m2 == "t" || m2 == "yes")
			default:
				confErr(fmt.Errorf("unknown config var %q\n", m2))
			}
		} else if !cfgEmptyPat.Match(b) {
			confErr(fmt.Errorf("mismatch line %q\n", b))
		}
	}
	confErr(scanner.Err())
}

func confErr(err error) {
	if err != nil {
		fmt.Printf("Reading wiki.conf: %v\n", err)
		os.Exit(1)
	}
}
