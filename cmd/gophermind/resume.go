package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gophermind/internal/session"
)

// choosePicked maps a user's picker input to a session id. "", "0", or "n"
// means "start fresh" (empty id). A 1-based index selects that session. Anything
// else is an error.
func choosePicked(infos []session.Info, choice string) (string, error) {
	choice = strings.TrimSpace(choice)
	if len(infos) == 0 || choice == "" || choice == "0" || strings.EqualFold(choice, "n") {
		return "", nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil {
		return "", fmt.Errorf("invalid selection %q", choice)
	}
	if n < 1 || n > len(infos) {
		return "", fmt.Errorf("selection %d out of range (1..%d)", n, len(infos))
	}
	return infos[n-1].ID, nil
}

// pickSession lists saved sessions and prompts for one to resume. It returns the
// chosen session id, or "" to start fresh. No sessions => "" with no prompt.
func pickSession(r *bufio.Reader, w io.Writer) (string, error) {
	infos, err := session.List()
	if err != nil || len(infos) == 0 {
		return "", err
	}
	fmt.Fprintln(w, "Resume a session? (enter to start fresh)")
	for i, s := range infos {
		fmt.Fprintf(w, "  %d) %-22s %s  %s\n", i+1, s.ID, s.ModTime.Format("2006-01-02 15:04"), s.Title)
	}
	fmt.Fprint(w, "> ")
	line, _ := r.ReadString('\n')
	return choosePicked(infos, line)
}
