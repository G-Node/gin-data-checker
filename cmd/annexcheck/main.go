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

func hasannexbranch(repo *git.Repository) bool {
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

// openrepo attempts to open a repository at the given path and if successful,
// checks for the existence of an annex branch and returns a populated
// repository with the Annex flag set.  If the path is not a repository, it
// returns nil.
func openrepo(path string) *repository {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil
	}

	return &repository{Path: path, Annex: hasannexbranch(repo)}
}

func scan(repostore string) []repository {
	repos := make([]repository, 0, 100)

	walker := func(path string, info os.FileInfo, err error) error {
		if filepath.Base(path) == git.GitDirName {
			// don't descend into .git directories
			return filepath.SkipDir
		}

		repo := openrepo(path)
		if repo != nil {
			repos = append(repos, *repo)
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
	// We could check repositories as we find them, but the initial scan is
	// fast enough that we can do it separately and it gives us a total count
	// so we can have some idea of the progress later when we're checking
	// files.
	repos := scan(c.Repostore)

	var annexcount uint64
	for _, r := range repos {
		if r.Annex {
			annexcount++
			fmt.Printf("%d: %s\n", annexcount, r.Path)
		}
	}

	fmt.Printf("Total repositories scanned:         %5d\n", len(repos))
	fmt.Printf("Repositories with git-annex branch: %5d\n", annexcount)
}
