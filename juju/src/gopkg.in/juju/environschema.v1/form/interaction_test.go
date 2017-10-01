package form_test

import (
	"fmt"
	"strconv"
	"strings"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
)

// newInteractionChecker returns a object that can be used to check a sequence of
// IO interactions. Expected input from the user is marked with the
// given user input marker (for example a distinctive unicode character
// that will not occur in the rest of the text) and runs to the end of a
// line.
//
// The returned interactionChecker is an io.ReadWriteCloser that checks that read
// and write corresponds to the expected action in the sequence.
//
// After all interaction is done, the interactionChecker should be closed to
// check that no more interactions are expected.
//
// Any failures will result in c.Fatalf being called.
//
// For example given the interactionChecker created with:
//
//		checker := newInteractionChecker(c, "»",  `What is your name: »Bob
//	And your age: »148
//	You're very old, Bob!
//	`)
//
// The following code will pass the checker:
//
//	fmt.Fprintf(checker, "What is your name: ")
//	buf := make([]byte, 100)
//	n, _ := checker.Read(buf)
//	name := strings.TrimSpace(string(buf[0:n]))
//	fmt.Fprintf(checker, "And your age: ")
//	n, _ = checker.Read(buf)
//	age, err := strconv.Atoi(strings.TrimSpace(string(buf[0:n])))
//	c.Assert(err, gc.IsNil)
//	if age > 90 {
//		fmt.Fprintf(checker, "You're very old, %s!\n", name)
//	}
//	checker.Close()
func newInteractionChecker(c *gc.C, userInputMarker, text string) *interactionChecker {
	var ios []ioInteraction
	for {
		i := strings.Index(text, userInputMarker)
		foundInput := i >= 0
		if i == -1 {
			i = len(text)
		}
		if i > 0 {
			ios = append(ios, ioInteraction{
				isInput: false,
				data:    text[0:i],
			})
			text = text[i:]
		}
		if !foundInput {
			break
		}
		text = text[len(userInputMarker):]
		endLine := strings.Index(text, "\n")
		if endLine == -1 {
			c.Errorf("no newline found after expected input %q", text)
		}
		ios = append(ios, ioInteraction{
			isInput: true,
			data:    text[0 : endLine+1],
		})
		text = text[endLine+1:]
	}
	return &interactionChecker{
		c:   c,
		ios: ios,
	}
}

type ioInteraction struct {
	isInput bool
	data    string
}

type interactionChecker struct {
	c   *gc.C
	ios []ioInteraction
}

// Read implements io.Reader by producing the next user
// input data from the interactionChecker. It raises an fatal error if
// the currently expected action is not a read.
func (c *interactionChecker) Read(buf []byte) (int, error) {
	if len(c.ios) == 0 {
		c.c.Fatalf("got read when expecting interaction to have finished")
	}
	io := &c.ios[0]
	if !io.isInput {
		c.c.Fatalf("got read when expecting write %q", io.data)
	}
	n := copy(buf, io.data)
	io.data = io.data[n:]
	if len(io.data) == 0 {
		c.ios = c.ios[1:]
	}
	return n, nil
}

// Write implements io.Writer by checking that the written
// data corresponds with the next expected text
// to be written.
func (c *interactionChecker) Write(buf []byte) (int, error) {
	if len(c.ios) == 0 {
		c.c.Fatalf("got write %q when expecting interaction to have finished", buf)
	}
	io := &c.ios[0]
	if io.isInput {
		c.c.Fatalf("got write %q when expecting read %q", buf, io.data)
	}
	if len(buf) > len(io.data) {
		c.c.Fatalf("write too long; got %q want %q", buf, io.data)
	}
	checkData := io.data[0:len(buf)]
	if string(buf) != checkData {
		c.c.Fatalf("unexpected write got %q want %q", buf, io.data)
	}
	io.data = io.data[len(buf):]
	if len(io.data) == 0 {
		c.ios = c.ios[1:]
	}
	return len(buf), nil
}

// Close implements io.Closer by checking that all expected interactions
// have been completed.
func (c *interactionChecker) Close() error {
	if len(c.ios) == 0 {
		return nil
	}
	io := &c.ios[0]
	what := "write"
	if io.isInput {
		what = "read"
	}
	c.c.Fatalf("filler terminated too early; expected %s %q", what, io.data)
	return nil
}

type interactionCheckerSuite struct{}

var _ = gc.Suite(&interactionCheckerSuite{})

func (*interactionCheckerSuite) TestNewIOChecker(c *gc.C) {
	checker := newInteractionChecker(c, "»", `What is your name: »Bob
And your age: »148
You're very old, Bob!
`)
	c.Assert(checker.ios, jc.DeepEquals, []ioInteraction{{
		data: "What is your name: ",
	}, {
		isInput: true,
		data:    "Bob\n",
	}, {
		data: "And your age: ",
	}, {
		isInput: true,
		data:    "148\n",
	}, {
		data: "You're very old, Bob!\n",
	}})
	fmt.Fprintf(checker, "What is your name: ")
	buf := make([]byte, 100)
	n, _ := checker.Read(buf)
	name := strings.TrimSpace(string(buf[0:n]))
	fmt.Fprintf(checker, "And your age: ")
	n, _ = checker.Read(buf)
	age, err := strconv.Atoi(strings.TrimSpace(string(buf[0:n])))
	c.Assert(err, gc.IsNil)
	if age > 90 {
		fmt.Fprintf(checker, "You're very old, %s!\n", name)
	}
	checker.Close()

	c.Assert(checker.ios, gc.HasLen, 0)
}
