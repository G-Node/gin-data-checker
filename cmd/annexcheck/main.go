package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

const usage = `
USAGE
  annexcheck <directory>

Scan a path recursively for annexed files with missing data

  <directory>    path to scan (recursively)

  -h, --help      display this help and exit
`

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
	// Array of annexed keys with missing contents
	MissingKeys []string
	// Embed git.Repository struct
	*git.Repository
}

func checkerr(err error) {
	if err != nil {
		log.Fatalf("[E] %s", err)
	}
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
			return storer.ErrStop
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

	return &repository{Path: path, Annex: hasannexbranch(repo), Repository: repo}
}

func scan(repostore string) []*repository {
	repos := make([]*repository, 0, 100)

	walker := func(path string, info os.FileInfo, err error) error {
		if filepath.Base(path) == git.GitDirName {
			// don't descend into .git directories
			return filepath.SkipDir
		}

		if !info.IsDir() {
			// Only check directories
			return nil
		}

		repo := openrepo(path)
		if repo != nil {
			repos = append(repos, repo)
		}
		return nil
	}

	err := filepath.Walk(repostore, walker)
	checkerr(err)

	return repos
}

func findMissingAnnex(repo *repository) {
	// blobs instead of the filesystem structure
	// this scanner needs to work with bare repositories, so we iterate the git
	blobs, err := repo.BlobObjects()
	if err != nil {
		log.Printf("[E] failed to get blobs for repository at %q: %s", repo.Path, err)
		return
	}

	checkblob := func(blob *object.Blob) error {
		if blob.Size > 1024 {
			// Annex pointer blobs are small
			// Skip blobs larger than 1k
			return nil
		}

		reader, err := blob.Reader()
		if err != nil {
			log.Printf("[E] failed to open blob %q for reading: %s", blob.Hash.String(), err)
			return nil
		}

		data := make([]byte, 1024)
		n, err := reader.Read(data)
		if err != nil {
			log.Printf("[E] failed to read contents of blob %q: %s", blob.Hash.String(), err)
			return nil
		}

		contents := string(data[:n])
		if strings.Contains(contents, "annex/objects") {
			repo.MissingKeys = append(repo.MissingKeys, strings.TrimSpace(contents))
		}
		return nil
	}

	err = blobs.ForEach(checkblob)
	if err != nil {
		log.Printf("[E] failed to check all blobs for repository %q: %s", repo.Path, err)
	}
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

	fmt.Print("Scanning annexed repositories for missing content... ")
	for _, r := range repos {
		if r.Annex {
			// TODO: Run async
			findMissingAnnex(r)
		}
	}
	fmt.Println("Done")

	for _, r := range repos {
		if len(r.MissingKeys) > 0 {
			fmt.Printf("Repository %q is missing content for the following files:\n", r.Path)
			for idx, annexkey := range r.MissingKeys {
				fmt.Printf("  %d: %s\n", idx, annexkey)
			}
			fmt.Println()
		}
	}
}
