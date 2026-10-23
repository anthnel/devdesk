// Package loopback gives tests a loopback port that code under test will bind
// later, without the window in which anything else can take it (DEVDESK-35).
//
// It imports testing and is meant to be imported from _test.go files only, so
// it never reaches the production binary.
package loopback
