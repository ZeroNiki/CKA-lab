// Command cka-lab serves the CKA trainer UI and verifies tasks against the lab cluster.
package main

import (
	"context"
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
	"cka-lab/internal/selftest"
	"cka-lab/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "selftest":
		os.Exit(selftestCmd(os.Args[2:]))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cka-lab serve [flags]\n       cka-lab selftest [flags] (--all | --level <id> | <task-id>...)")
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

func selftestCmd(args []string) int {
	fs := flag.NewFlagSet("selftest", flag.ExitOnError)
	var common commonFlags
	common.register(fs)
	all := fs.Bool("all", false, "test every task")
	level := fs.String("level", "", "test the tasks of one level")
	fs.Parse(args)

	c, err := content.Load(common.contentDir)
	if err != nil {
		log.Fatalf("content: %v", err)
	}
	var tasks []*content.Task
	switch {
	case *all:
		for _, l := range c.Levels {
			for _, t := range l.Tasks {
				tasks = append(tasks, c.TaskByID[t.ID])
			}
		}
	case *level != "":
		for _, l := range c.Levels {
			if l.ID == *level {
				for _, t := range l.Tasks {
					tasks = append(tasks, c.TaskByID[t.ID])
				}
			}
		}
		if len(tasks) == 0 {
			log.Fatalf("unknown level %q", *level)
		}
	case fs.NArg() > 0:
		for _, id := range fs.Args() {
			t, ok := c.TaskByID[id]
			if !ok {
				log.Fatalf("unknown task %q", id)
			}
			tasks = append(tasks, t)
		}
	default:
		usage()
	}

	lc := &lazyCluster{kubeconfig: common.kubeconfig, sshConfig: common.sshConfig}
	snap := cluster.VagrantSnapshotter{Dir: common.vagrantDir}
	deps := selftest.Deps{Prepare: lc.Prepare, Check: lc.Check, Exec: lc, Restore: snap.Restore}

	failed := 0
	ctx := context.Background()
	for _, t := range selftest.Order(tasks) {
		r := selftest.RunTask(ctx, deps, t)
		if r.OK() {
			fmt.Printf("PASS %s\n", t.ID)
			continue
		}
		failed++
		fmt.Printf("FAIL %s (%s)\n", t.ID, t.File)
		switch {
		case r.Err != nil:
			fmt.Printf("     %v\n", r.Err)
		case !r.FailedBeforeSolution:
			fmt.Println("     checks already pass before the solution: the task solves itself")
		}
		for _, res := range r.Results {
			if !res.Pass {
				fmt.Printf("     x %s: %s\n", res.Description, res.Actual)
			}
		}
	}
	fmt.Printf("%d/%d passed\n", len(tasks)-failed, len(tasks))
	if failed > 0 {
		return 1
	}
	return 0
}
