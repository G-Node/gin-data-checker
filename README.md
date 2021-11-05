# gin-data-checker

Command line tool to check for missing annex content in git/git-annex repositories.

```
USAGE
  annexcheck [options] <directory>

Scan a path recursively for annexed files with missing data

  <directory>    path to scan (recursively)

  --database     database to use for determining forks; if unspecified, no fork detection is performed
  --nworkers     number of concurrent workers (file scanners)

  -h, --help     display this help and exit
  --version      show version information
```
