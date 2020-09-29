package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// askForConfirmation asks the user for confirmation. A user must type in "y" or "n" and
// then press enter. It has fuzzy matching, so "y", "Y" both count as
// confirmations. If the input is not recognized, it will ask again. The function does not return
// until it gets a valid response from the user.
// taken from https://gist.github.com/r0l1/3dcbb0c8f6cfe9c66ab8008f55f8f28b
func AskForConfirmation(stdin io.Reader, message string) (bool, error) {
	reader := bufio.NewReader(stdin)

	for {
		fmt.Printf("%s [y/n]: ", message)

		response, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}

		response = strings.ToLower(strings.TrimSpace(response))

		if response == "y" {
			return true, nil
		} else if response == "n" {
			return false, nil
		}
	}
}
