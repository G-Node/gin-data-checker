package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

const usage = `
USAGE
  annexcheck <directory>

Scan a path recursively for annexed files with missing data

  <directory>    path to scan (recursively)

  -h, --help      display this help and exit
`

func checkerr(err error) {
	if err != nil {
		log.Fatalf("[E] %v", err.Error())
	}
}

type config struct {
	// Repostore to scan (recursively)
	Repostore string
}

// represents a single repository
type repository struct {
	// Location of the repository (absolute path)
	Path string
	// True if it contains annex branches
	Annex bool
}

func printusage() {
	fmt.Println(usage)
	os.Exit(0)
}

func getargs() config {
	args := os.Args
	if len(args) != 2 {
		printusage()
	} else if args[1] == "-h" || args[1] == "--help" {
		printusage()
	}

	return config{Repostore: args[1]}
}

func hasannexbranch(path string) bool {
	repo, err := git.PlainOpen(path)
	checkerr(err)

	refs, err := repo.Branches()
	checkerr(err)

	found := false
	isannexref := func(c *plumbing.Reference) error {
		if string(c.Name()) == "refs/heads/git-annex" {
			found = true
		}
		return nil
	}
	err = refs.ForEach(isannexref)
	checkerr(err)
	return found
}

func scan(repostore string) []repository {
	repos := make([]repository, 0, 100)

	walker := func(path string, info os.FileInfo, err error) error {
		// stupid git detection: If path is a directory called .git, the parent is a repository
		if filepath.Base(path) == git.GitDirName && info.IsDir() {
			gitroot := filepath.Dir(path)
			repos = append(repos, repository{Path: gitroot, Annex: hasannexbranch(gitroot)})
		}
		return nil
	}

	err := filepath.Walk(repostore, walker)
	checkerr(err)

	return repos
}

func main() {
	c := getargs()
	fmt.Printf("Scanning %s\n", c.Repostore)
	repos := scan(c.Repostore)

	fmt.Printf("Found %d repositories\n", len(repos))
}
