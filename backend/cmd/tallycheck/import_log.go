package main

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var nativeImportBlock = regexp.MustCompile(`(?s)^Importing Data from '([^'\r\n]+)' on ([^\r\n]+)\n\s*Importing Data to company: '([^'\r\n]+)'\n\s*Import Summary\nCreated\s*:\s*([0-9]+)\nAltered\s*:\s*([0-9]+)\nDeleted\s*:\s*([0-9]+)\nCombined\s*:\s*([0-9]+)\nIgnored\s*:\s*([0-9]+)\nErrors\s*:\s*([0-9]+)\nExceptions\s*:\s*([0-9]+)$`)

func checkImportLog(b []byte, importedFile, company string, expected int) error {
	if strings.TrimSpace(importedFile) == "" {
		return errors.New("native log requires the exact --imported-file path used in Tally")
	}
	decoded, _, err := decodeObserved(b, "tallyprime")
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(decoded), "\r\n", "\n")
	blocks := strings.Split(text, "----------------------------------------------")
	if strings.TrimSpace(blocks[len(blocks)-1]) != "" {
		return errors.New("native import log is truncated")
	}
	matches := 0
	for _, block := range blocks[:len(blocks)-1] {
		m := nativeImportBlock.FindStringSubmatch(strings.TrimSpace(block))
		if m == nil {
			return errors.New("unsupported or malformed native import log block")
		}
		if m[1] != importedFile {
			continue
		}
		matches++
		if m[3] != company {
			return errors.New("native import log company differs from declared company")
		}
		for i, raw := range m[4:] {
			count, err := strconv.Atoi(raw)
			want := 0
			if i == 0 {
				want = expected
			}
			if err != nil || count != want {
				return errors.New("native import log reports unexpected counts or effects")
			}
		}
	}
	if matches != 1 {
		return errors.New("native log must contain exactly one import of the declared file")
	}
	return nil
}
