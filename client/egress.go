package main

import "regexp"

var clientEgressIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,15}$`)

func validEgressID(id string) bool { return clientEgressIDPattern.MatchString(id) }
