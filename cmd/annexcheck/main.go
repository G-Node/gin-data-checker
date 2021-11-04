package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

var (
	appversion string
	build      string
	commit     string
)

const usage = `
USAGE
  annexcheck [options] <directory>

Scan a path recursively for annexed files with missing data

  <directory>    path to scan (recursively)

  --database     database to use for determining forks; if unspecified, no fork detection is performed
  --nworkers     number of concurrent workers (file scanners)

  -h, --help     display this help and exit
  --version      show version information
`

const annexDirLetters = "0123456789zqjxkmvwgpfZQJXKMVWGPF"

// represents a single repository
type repository struct {
	// Location of the repository (absolute path)
	Path string
	// True if it contains annex branches
	Annex bool
	// True if the repository is a fork (always false if no database is used)
	Fork bool
	// Array of annexed keys with missing contents
	MissingContent []annexedfile
	// Embed git.Repository struct
	*git.Repository
}

type annexedfile struct {
	// The path to the annexed file contents, rooted at the .git/ directory
	// (starts with /annex/objects)
	ObjectPath string
	// The path in the repository tree, rooted at the repository root
	TreePath string
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

func printversion() {
	if appversion == "" {
		appversion = "[dev build]"
		build = "[dev]"
		commit = "???"
	}

	fmt.Printf("GIN data checker %s Build %s (%s)\n", appversion, build, commit)
	os.Exit(0)
}

// openrepo attempts to open a repository at the given path and if successful,
// returns a repository with a reference to the open git.Repository.
func openrepo(path string) *repository {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil
	}

	return &repository{Path: path, Repository: repo}
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

func isfork(path string, database *gindb) bool {
	// infer unique repo name from path
	path = strings.TrimSuffix(path, ".git")
	pathparts := strings.Split(path, string(filepath.Separator))
	nparts := len(pathparts)
	if nparts < 2 {
		log.Printf("Couldn't infer unique repo name from parts: %+v\n", pathparts)
		// can't figure out unique repo name; just give up
		return false
	}
	repopath := strings.ToLower(filepath.Join(pathparts[nparts-2:]...)) // last two path components
	repo, ok := database.Repositories[repopath]
	if !ok {
		log.Printf("Couldn't find repository %q in database", repopath)
		return false
	}
	return repo.IsFork
}

func scan(cfg config) []*repository {
	repos := make([]*repository, 0, 100)

	var database *gindb
	if cfg.Database != "" {
		database = loaddb(cfg.Database)
	}

	walker := func(path string, info os.FileInfo, err error) error {
		if filepath.Base(path) == git.GitDirName {
			// don't descend into .git directories
			return filepath.SkipDir
		}

		if info == nil {
			return fmt.Errorf("could not access directory '%s'", path)
		}

		if !info.IsDir() {
			// Only check directories
			return nil
		}

		repo := openrepo(path)
		if repo == nil {
			return nil
		}

		repo.Annex = hasannexbranch(repo.Repository)
		if database != nil {
			repo.Fork = isfork(path, database)
		}
		repos = append(repos, repo)
		return nil
	}

	err := filepath.Walk(cfg.Repostore, walker)
	checkerr(err)

	return repos
}

// hashdirlower is the new method for calculating the location of an annexed
// file's contents based on its key
// See https://git-annex.branchable.com/internals/hashing/ for description
func hashdirlower(key string) string {
	hash := md5.Sum([]byte(key))
	hashstr := fmt.Sprintf("%x", hash)
	return filepath.Join(hashstr[:3], hashstr[3:6], key)
}

// hashdirmixed is the old method for calculating the location of an annexed
// file's contents based on its key
// See https://git-annex.branchable.com/internals/hashing/ for description
func hashdirmixed(key string) string {
	hash := md5.Sum([]byte(key))
	var sum uint64

	sum = 0
	// reverse the first 32bit word of the hash
	firstWord := make([]byte, 4)
	for idx, b := range hash[:4] {
		firstWord[3-idx] = b
	}
	for _, b := range firstWord {
		sum <<= 8
		sum += uint64(b)
	}

	rem := sum
	letters := make([]byte, 4)
	idx := 0
	for rem > 0 && idx < 4 {
		// pull out five bits
		chr := rem & 31
		// save it
		letters[idx] = annexDirLetters[chr]
		// shift the remaining
		rem >>= 6
		idx++
	}

	path := filepath.Join(fmt.Sprintf("%s%s", string(letters[1]), string(letters[0])), fmt.Sprintf("%s%s", string(letters[3]), string(letters[2])), key)
	return path
}

func checkblob(objectstore string, blob *object.Blob, fileloc string) (annexedfile, error) {
	if blob.Size > 1024 {
		// Annex pointer blobs are small
		// Skip blobs larger than 1k
		return annexedfile{}, fmt.Errorf("skip")
	}

	if blob.Size == 0 {
		// skip empty files too
		return annexedfile{}, fmt.Errorf("skip")
	}

	reader, err := blob.Reader()
	if err != nil {
		log.Printf("[E] failed to open blob %q for reading: %s", blob.Hash.String(), err)
		return annexedfile{}, fmt.Errorf("skip")
	}

	data := make([]byte, 1024)
	n, err := reader.Read(data)
	if err != nil {
		log.Printf("[E] failed to read contents of blob %q: %s", blob.Hash.String(), err)
		return annexedfile{}, fmt.Errorf("skip")
	}

	contents := string(data[:n])
	if strings.Contains(contents, "annex/objects") {
		// calculate the content location and check if it exists
		key := filepath.Base(strings.TrimSpace(contents))

		// there are two possible object paths depending on annex version
		// the most common one is the newest, but we should try both anyway
		objectpath := filepath.Join(objectstore, hashdirmixed(key), key)
		if _, err := os.Stat(objectpath); os.IsNotExist(err) {
			// try the other one
			objectpath = filepath.Join(objectstore, hashdirlower(key), key)
			if _, err = os.Stat(objectpath); os.IsNotExist(err) {
				return annexedfile{ObjectPath: objectpath, TreePath: fileloc}, nil
			}
		}
	}
	return annexedfile{}, fmt.Errorf("skip")
}

func findMissingAnnex(repo *repository) {
	// blobs instead of the filesystem structure
	// this scanner needs to work with bare repositories, so we iterate the git
	head, err := repo.Head()
	if err != nil {
		log.Printf("[E] failed to get head for repository at %q: %s", repo.Path, err)
		return
	}

	headcommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		log.Printf("[E] failed to get HEAD commit for repository at %q: %s", repo.Path, err)
		return
	}

	tree, err := headcommit.Tree()
	if err != nil {
		log.Printf("[E] failed to get root HEAD tree for repository at %q: %s", repo.Path, err)
		return
	}

	walker := object.NewTreeWalker(tree, true, nil)
	for name, entry, err := walker.Next(); err != io.EOF; name, entry, err = walker.Next() {
		switch entry.Mode {
		case filemode.Regular:
			fallthrough
		case filemode.Symlink:
			fallthrough
		case filemode.Executable:
			blob, err := repo.BlobObject(entry.Hash)
			if err != nil {
				log.Printf("[E] failed to get blob for %q (%s) in %q: %s", entry.Hash, name, repo.Path, err)
				continue
			}
			gitdir := git.GitDirName
			_, err = repo.Worktree()
			if err == git.ErrIsBareRepository {
				gitdir = ""
			}
			objectstore := filepath.Join(repo.Path, gitdir, "annex", "objects")
			latest, err := checkblob(objectstore, blob, name)
			if err == nil {
				repo.MissingContent = append(repo.MissingContent, latest)
			}
		}
	}
}

func main() {
	c := readargs()
	if c.Database != "" {
		fmt.Printf("Using %s database to detect forks\n", c.Database)
	}
	fmt.Printf("Scanning %s\n", c.Repostore)
	// We could check repositories as we find them, but the initial scan is
	// fast enough that we can do it separately and it gives us a total count
	// so we can have some idea of the progress later when we're checking
	// files.
	repos := scan(c)

	var jobcount uint64
	for _, r := range repos {
		if r.Annex {
			fmt.Printf("%d: %s", jobcount, r.Path)
			if r.Fork {
				fmt.Print(" is a fork (skipping)")
			} else {
				jobcount++
				fmt.Print(" will be scanned")
			}
			fmt.Println()
		}
	}

	fmt.Printf("Total repositories found:         %5d\n", len(repos))
	fmt.Printf("Repositories to scan (annexed, not forks): %5d\n", jobcount)

	wq := newworkerqueue(c.NWorkers, jobcount)
	wq.start()
	fmt.Printf("Submitting %d jobs... ", jobcount)
	for _, r := range repos {
		if r.Annex && !r.Fork {
			wq.submitjob(r)
		}
	}
	fmt.Println("OK")

	wq.wait()

	for _, r := range repos {
		if len(r.MissingContent) > 0 {
			repoconf, err := r.Config()
			if err != nil {
				fmt.Printf("could not parse remotes: %s", err.Error())
			} else {
				fmt.Printf("\nRemotes %v\n", repoconf.Remotes["origin"].URLs)
			}
			fmt.Printf("Repository %q is missing content for the following files:\n", r.Path)
			for idx, af := range r.MissingContent {
				fmt.Printf("  %d: %s [%s]\n", idx+1, af.TreePath, af.ObjectPath)
			}
			fmt.Println()
		}
	}
}
