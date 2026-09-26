package forge

import (
	"context"
	"fmt"

	"github.com/y3owk1n/oku/internal/shape"
)

// checked refuses an answer of a forge that lacks what oku reads from it, such
// as an answer in a format the forge has changed.
type checked struct {
	Forge

	// name is the forge in errors, such as "GitHub".
	name string
}

func (c checked) Head(ctx context.Context, repo string) (string, error) {
	commit, err := c.Forge.Head(ctx, repo)
	if err == nil {
		err = shape.Check(fmt.Sprintf("%s's answer for the newest commit of %s", c.name, repo),
			shape.Field{Name: "the commit", Has: commit != ""})
	}

	return commit, err
}

func (c checked) Release(ctx context.Context, repo, tag string) (Release, error) {
	release, err := c.Forge.Release(ctx, repo, tag)
	if err == nil {
		err = release.check(c.name, repo)
	}

	return release, err
}

func (c checked) Releases(ctx context.Context, repo string) ([]Release, error) {
	releases, err := c.Forge.Releases(ctx, repo)

	for _, release := range releases {
		if err == nil {
			err = release.check(c.name, repo)
		}
	}

	return releases, err
}

func (c checked) Tags(ctx context.Context, repo string) ([]string, error) {
	tags, err := c.Forge.Tags(ctx, repo)

	for _, tag := range tags {
		if err == nil {
			err = shape.Check(fmt.Sprintf("%s's answer for the tags of %s", c.name, repo),
				shape.Field{Name: "the name of a tag", Has: tag != ""})
		}
	}

	return tags, err
}

func (c checked) TagCommit(ctx context.Context, repo, tag string) (Commit, error) {
	commit, err := c.Forge.TagCommit(ctx, repo, tag)
	if err == nil {
		err = shape.Check(fmt.Sprintf("%s's answer for the tag %s of %s", c.name, tag, repo),
			shape.Field{Name: "the commit", Has: commit.SHA != ""})
	}

	return commit, err
}

// check refuses a release without a tag, or with a file that has no name or
// URL.
func (r Release) check(forge, repo string) error {
	named, linked := true, true

	for _, a := range r.Assets {
		named = named && a.Name != ""
		linked = linked && a.URL != ""
	}

	return shape.Check(
		fmt.Sprintf("%s's answer for the releases of %s", forge, repo),
		shape.Field{Name: "a tag", Has: r.Tag != ""},
		shape.Field{Name: "the name of a file", Has: named},
		shape.Field{Name: "the URL of a file", Has: linked},
	)
}
