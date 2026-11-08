// Command cka-lab serves the CKA trainer UI and verifies tasks against the lab cluster.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"time"

	"cka-lab/internal/api"
	"cka-lab/internal/cluster"
	"cka-lab/internal/content"
	"cka-lab/internal/game"
	"cka-lab/internal/progress"
	"cka-lab/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cka-lab serve [flags]")
	os.Exit(2)
}

type commonFlags struct {
	contentDir, kubeconfig, sshConfig, vagrantDir string
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.contentDir, "content", "content", "content directory")
	fs.StringVar(&c.kubeconfig, "kubeconfig", ".lab/kubeconfig", "kubeconfig written by ./lab up")
	fs.StringVar(&c.sshConfig, "ssh-config", ".lab/ssh_config", "vagrant ssh-config output")
	fs.StringVar(&c.vagrantDir, "vagrant-dir", ".", "directory with the Vagrantfile")
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var common commonFlags
	common.register(fs)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	configPath := fs.String("config", "config.yaml", "coins and shop config")
	statePath := fs.String("state", ".lab/progress.json", "progress file")
	fs.Parse(args)

	c, err := content.Load(common.contentDir)
	if err != nil {
		log.Fatalf("content: %v", err)
	}
	cfg, err := game.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := progress.Open(*statePath)
	if err != nil {
		log.Fatalf("progress: %v", err)
	}
	lc := &lazyCluster{kubeconfig: common.kubeconfig, sshConfig: common.sshConfig}
	snap := cluster.VagrantSnapshotter{Dir: common.vagrantDir}
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
	svc := game.NewService(c, cfg, st, lc, lc, snap, rng, time.Now)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.New(svc, web.FS),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("CKA Lab on http://%s (%d levels)", *addr, len(c.Levels))
	log.Fatal(srv.ListenAndServe())
}
