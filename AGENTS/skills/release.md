# Release Skill

Use this procedure when asked to make a release, submit for review, or cut a version.

## Pre-release Checklist

1. Confirm all planned changes are committed to `main`.
2. Run `go test ./...` — must pass with zero failures.
3. Run `go vet ./...` — must pass.
4. Run `bash -n install.sh uninstall.sh deploy/dev-deploy.sh deploy/dev-uninstall.sh deploy/on_boot_hook.sh` — must pass syntax check.
5. Check `AGENTS/progress.md` — update sprint status if any sprints completed this session.

## Versioning

Follow semantic versioning (`v<major>.<minor>.<patch>`). Patch: bug fixes. Minor: new features, no breaking API changes. Major: breaking changes.

Check the latest tag: `git tag --sort=-version:refname | head -5`

## Release Steps

1. Ensure `main` branch is clean and all tests pass (see pre-release checklist above).
2. Create and push the version tag:
   ```bash
   git tag v<version>
   git push origin v<version>
   ```
3. The GitHub Actions workflow (`.github/workflows/build.yml`) triggers automatically on tag push:
   - Runs `go test ./...`
   - Builds `split-vpn-webui-linux-amd64` and `split-vpn-webui-linux-arm64`
   - Generates `SHA256SUMS`
   - Creates GitHub Release with all artifacts and generated release notes
4. Verify the workflow run succeeded: `gh run list --limit 5`
5. Verify the release assets are present: `gh release view v<version>`

## After Release

Update `AGENTS/progress.md` with the release tag and any relevant session notes.
