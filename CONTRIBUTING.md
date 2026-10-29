- Clone the repo
- Install go: https://go.dev/doc/install
- TODO: How to run dev version of pigo...


## Releasing

Push a version tag. The github workflow (`.github/workflows/release.yml`) will build and publish it.

    git tag v0.1.0
    git push origin v0.1.0

Tags containing a `-` (like `v0.1.0-test`) are published as pre-releases.


## Deleting a release

1. On github, open Releases, open the release and delete it.
2. Delete the tag on github and locally:

       git push origin --delete v0.1.0
       git tag -d v0.1.0
