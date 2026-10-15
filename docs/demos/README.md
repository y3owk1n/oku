# Demos

Each clip runs oku in a throwaway home on macOS. The guide linked under each
clip explains its commands.

## Install from GitHub

![oku add installs ripgrep and fd from GitHub repos that have no manifest, and writes both to oku.toml](01-install.gif)

Guide: [Add packages](../guides/add-packages.md)

## Install from aqua and Homebrew casks

![oku add installs jq from the aqua registry and the Stats app from its Homebrew cask](02-recipes.gif)

Guide: [Add packages](../guides/add-packages.md#add-a-package-from-homebrew-scoop-winget-or-aqua)

## Install from npm and PyPI

![oku sync installs prettier from npm and ruff from PyPI, on the node and python that [runtimes] names, after one approval](03-registries.gif)

Guide: [npm, PyPI, Go and cargo packages](../guides/npm-pypi-go-cargo.md)

## Try a tool without installing it

![oku shell and oku run use ripgrep and hyperfine without installing them](04-try.gif)

Guide: [Tools per project](../guides/projects.md#try-a-package-without-adding-it)

## Set up a new machine

![A new machine clones its machines repo to ~/.config/oku, and oku sync installs four packages and a dotfile](05-new-machine.gif)

Guide: [Set up a machine from a repo](../guides/new-machine.md)

## Place dotfiles and templates

![oku sync links a vimrc and renders a ghostty config from [vars], then writes it again when a variable changes](06-dotfiles.gif)

Guide: [Manage dotfiles](../guides/dotfiles.md)

## Place secrets

![oku sync decrypts a token from a sops file into a gh config](07-secrets.gif)

Guide: [Keep secrets](../guides/secrets.md)

## Set and roll back OS settings

![oku sync writes two macOS defaults, and oku rollback removes them](08-os-settings.gif)

Guide: [Change OS settings](../guides/os-settings.md)

## Run a service

![oku sync --dry-run shows the launchd service it would write for atuin](09-services.gif)

Guide: [Run a service](../guides/services.md)

## Give a project its own tools

![Inside ~/api, after oku allow and oku sync, the project's just and DATABASE_URL apply, and both are gone after cd ~](10-projects.gif)

Guide: [Tools per project](../guides/projects.md)

## Update and roll back

![oku update moves ripgrep from 14.1.1 to 15.2.0, and oku rollback puts 14.1.1 back](11-history.gif)

Guide: [Undo and clean up](../guides/undo-and-clean-up.md)

## Write a manifest

![oku manifest init writes a manifest for hyperfine from its releases, and lint and test check it](12-publish.gif)

Guide: [Publish a manifest](../guides/publish-a-manifest.md)

## Check the machine

![oku doctor, oku which, oku outdated, oku verify and oku du](13-diagnose.gif)

Guide: [Troubleshooting](../troubleshooting.md)
