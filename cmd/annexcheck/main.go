package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"math"
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
  annexcheck <directory>

Scan a path recursively for annexed files with missing data

  <directory>    path to scan (recursively)

  -h, --help     display this help and exit
  --version      show version information
`

const annexDirLetters = "0123456789zqjxkmvwgpfZQJXKMVWGPF"

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

func getargs() config {
	args := os.Args
	if len(args) != 2 {
		printusage()
	} else if args[1] == "-h" || args[1] == "--help" {
		printusage()
	} else if args[1] == "--version" {
		printversion()
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

func pow(x, y int) int {
	return int(math.Pow(float64(x), float64(y)))
}

func myencoding(b []byte) {
	x := uint16(b[1]) + uint16(b[0])<<8
	fmt.Println(x)
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

	gitdir := git.GitDirName
	_, err = repo.Worktree()
	if err == git.ErrIsBareRepository {
		gitdir = ""
	}
	objectstore := filepath.Join(repo.Path, gitdir, "annex", "objects")

	checkblob := func(blob *object.Blob, fileloc string) {
		if blob.Size > 1024 {
			// Annex pointer blobs are small
			// Skip blobs larger than 1k
			return
		}

		if blob.Size == 0 {
			// skip empty files too
			return
		}

		reader, err := blob.Reader()
		if err != nil {
			log.Printf("[E] failed to open blob %q for reading: %s", blob.Hash.String(), err)
			return
		}

		data := make([]byte, 1024)
		n, err := reader.Read(data)
		if err != nil {
			log.Printf("[E] failed to read contents of blob %q: %s", blob.Hash.String(), err)
			return
		}

		contents := string(data[:n])
		if strings.Contains(contents, "annex/objects") {
			// calculate the content location and check if it exists
			key := filepath.Base(strings.TrimSpace(contents))

			// there are two possible object paths depending on annex version
			// the most common one is the newest, but we should try both anyway
			objectpath := filepath.Join(objectstore, hashdirmixed(key))
			if _, err := os.Stat(objectpath); os.IsNotExist(err) {
				// try the other one
				objectpath = filepath.Join(objectstore, hashdirlower(key))
				if _, err = os.Stat(objectpath); os.IsNotExist(err) {
					repo.MissingContent = append(repo.MissingContent, annexedfile{ObjectPath: objectpath, TreePath: fileloc})
				}
			}
		}
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
			checkblob(blob, name)
		}
	}
}

type workerqueue struct {
	queue     chan *repository
	nworkers  uint8
	njobs     uint64
	ncomplete uint64
}

func newworkerqueue(nworkers uint8, njobs uint64) *workerqueue {
	wq := workerqueue{}
	wq.queue = make(chan *repository, njobs)
	wq.nworkers = nworkers
	wq.njobs = njobs

	return &wq
}

func (wq *workerqueue) start() {
	for idx := uint8(0); idx < wq.nworkers; idx++ {
		go wq.startworker()
		fmt.Printf("Worker %d started\n", idx)
	}
}

func (wq *workerqueue) wait() {
	for wq.ncomplete < wq.njobs {
		fmt.Printf(" : %d/%d\r", wq.ncomplete, wq.njobs)
	}
	close(wq.queue)
	fmt.Printf("\n%d jobs complete. Stopping workers.\n", wq.ncomplete)
}

func (wq *workerqueue) startworker() {
	for repo := range wq.queue {
		findMissingAnnex(repo)
		wq.ncomplete++
	}
}

func (wq *workerqueue) submitjob(r *repository) {
	wq.queue <- r
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

	wq := newworkerqueue(8, annexcount)
	wq.start()
	fmt.Printf("Submitting %d jobs...", annexcount)
	for _, r := range repos {
		if r.Annex {
			// TODO: Run async
			// findMissingAnnex(r)
			wq.submitjob(r)
		}
	}
	fmt.Println("Done")

	wq.wait()

	for _, r := range repos {
		if len(r.MissingContent) > 0 {
			fmt.Printf("Repository %q is missing content for the following files:\n", r.Path)
			for idx, af := range r.MissingContent {
				fmt.Printf("  %d: %s [%s]\n", idx+1, af.TreePath, af.ObjectPath)
			}
			fmt.Println()
		}
	}
}
