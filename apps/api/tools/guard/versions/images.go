package versions

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/guard/report"
)

var (
	imageDigest  = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	threeNumbers = regexp.MustCompile(`^v?\d+\.\d+\.\d+($|[-.+_])`)
	twoNumbers   = regexp.MustCompile(`^v?\d+\.\d+($|[-.+_])`)
	stageNumber  = regexp.MustCompile(`^\d+$`)
)

// twoNumberImages lists the images whose full version has two numbers, by
// the last part of their name. Every other image needs three: `caddy:2.10`
// floats over its patches. An image with another scheme is pinned by its
// digest.
var twoNumberImages = map[string]bool{
	"postgres": true,
}

// isExactImage tells whether an image reference names one version: a digest,
// or a tag that starts with the full version of the image.
func isExactImage(reference string) bool {
	if imageDigest.MatchString(reference) {
		return true
	}
	name, tag := splitImage(reference)
	if twoNumberImages[path.Base(name)] {
		return twoNumbers.MatchString(tag)
	}
	return threeNumbers.MatchString(tag)
}

// splitImage separates the name of an image from its tag. The colon of a
// registry port is not a tag: the tag follows the last slash.
func splitImage(reference string) (name, tag string) {
	reference, _, _ = strings.Cut(reference, "@")
	slash := strings.LastIndex(reference, "/")
	colon := strings.LastIndex(reference, ":")
	if colon <= slash {
		return reference, ""
	}
	return reference[:colon], reference[colon+1:]
}

func imageFinding(line int, reference string) report.Finding {
	return report.Finding{
		Line: line,
		Message: fmt.Sprintf("image %q names no exact version: write the full version in its tag, or pin the image by its digest%s",
			reference, pointer),
	}
}

// judgeImage returns a finding when the node names an image that is not
// exact.
func judgeImage(node *yaml.Node) []report.Finding {
	if !isText(node) || isExactImage(node.Value) {
		return nil
	}
	return []report.Finding{imageFinding(node.Line, node.Value)}
}

// compose judges every `image` of a Compose file, the ones of an extension
// field included.
func compose(content []byte) ([]report.Finding, error) {
	document, err := parse(content)
	if err != nil {
		return nil, err
	}
	var findings []report.Finding
	for _, mapping := range mappings(document) {
		findings = append(findings, judgeService(mapping)...)
	}
	return findings, nil
}

// judgeService judges the image of a service. A service with `build` names
// the image it builds, and the Dockerfile pins what that image is built on.
// So does an image this repository publishes, which a server names by the
// commit it was built from.
func judgeService(service *yaml.Node) []report.Finding {
	image := child(service, "image")
	if child(service, "build") != nil || isText(image) && publishedHere.MatchString(image.Value) {
		return nil
	}
	return judgeImage(image)
}

// publishedHere is an image this repository builds and publishes to the
// GitHub container registry (Publishing in CODING_STANDARDS.md), the API,
// Caddy with the built web app or the seed, tagged with one variable of
// Compose, `${NAME}`, with or without a default or an error after the name:
// the variable that holds the hash of the commit it was built from.
var publishedHere = regexp.MustCompile(`^ghcr\.io/dannilodms/software-factory-manager-[a-z]+:\$\{[A-Za-z_][A-Za-z0-9_]*(:?[-?+][^}]*)?\}$`)

// dockerfile judges the image of every `FROM` and of every `COPY --from`
// that is not an earlier stage, and what xcaddy builds into Caddy.
func dockerfile(content []byte) ([]report.Finding, error) {
	stages := map[string]bool{"scratch": true}
	var findings []report.Finding
	for number, line := range strings.Split(string(content), "\n") {
		read := instructionOf(strings.Fields(line))
		findings = append(findings, read.judge(number+1, stages)...)
		// A stage is known from the line after the one that names it.
		stages[read.stage] = true
	}
	return append(findings, xcaddyBuilds(string(content))...), nil
}

// instruction is what one line of a Dockerfile says about images.
type instruction struct {
	// images are what a `FROM` builds on and what a `COPY --from` reads.
	images []string
	// stage is the name a `FROM` gives its stage, in lower case.
	stage string
}

func instructionOf(words []string) instruction {
	if len(words) == 0 {
		return instruction{}
	}
	switch strings.ToUpper(words[0]) {
	case "FROM":
		return fromOf(words[1:])
	case "COPY":
		return instruction{images: copySources(words[1:])}
	}
	return instruction{}
}

// fromOf reads `FROM [--platform=...] image [AS stage]`.
func fromOf(arguments []string) instruction {
	plain := slices.DeleteFunc(slices.Clone(arguments), func(argument string) bool {
		return strings.HasPrefix(argument, "--")
	})
	if len(plain) == 0 {
		return instruction{}
	}
	read := instruction{images: plain[:1]}
	if len(plain) == 3 && strings.EqualFold(plain[1], "AS") {
		read.stage = strings.ToLower(plain[2])
	}
	return read
}

// copySources reads the `--from` of a `COPY`.
func copySources(arguments []string) []string {
	var sources []string
	for _, argument := range arguments {
		sources = append(sources, copySource(argument)...)
	}
	return sources
}

// copySource returns what a `--from` names when it is not a stage given by
// its number.
func copySource(argument string) []string {
	source, isFrom := strings.CutPrefix(argument, "--from=")
	if !isFrom || stageNumber.MatchString(source) {
		return nil
	}
	return []string{source}
}

func (i instruction) judge(line int, stages map[string]bool) []report.Finding {
	var findings []report.Finding
	for _, reference := range i.images {
		findings = append(findings, judgeReference(line, reference, stages)...)
	}
	return findings
}

func judgeReference(line int, reference string, stages map[string]bool) []report.Finding {
	if stages[strings.ToLower(reference)] || isExactImage(reference) {
		return nil
	}
	return []report.Finding{imageFinding(line, reference)}
}
