// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agree_test

import (
	"sync"
	"testing"

	"github.com/juju/cmd/cmdtesting"
	jujutesting "github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/romulus/api/terms"
	"github.com/juju/romulus/cmd/agree"
)

func TestPackage(t *testing.T) {
	gc.TestingT(t)
}

var _ = gc.Suite(&agreeSuite{})

var testTerms = "Test Terms"

type agreeSuite struct {
	client *mockClient
}

func (s *agreeSuite) SetUpTest(c *gc.C) {
	s.client = &mockClient{}

	jujutesting.PatchValue(agree.ClientNew, func(...terms.ClientOption) (terms.Client, error) {
		return s.client, nil
	})
}

func (s *agreeSuite) TestAgreementNothingToSign(c *gc.C) {
	jujutesting.PatchValue(agree.UserAnswer, func() (string, error) {
		return "y", nil
	})

	s.client.user = "test-user"
	s.client.setUnsignedTerms([]terms.GetTermsResponse{})

	ctx, err := cmdtesting.RunCommand(c, agree.NewAgreeCommand(), "test-term/1")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `Already agreed
`)
}
func (s *agreeSuite) TestAgreement(c *gc.C) {
	var answer string
	jujutesting.PatchValue(agree.UserAnswer, func() (string, error) {
		return answer, nil
	})

	s.client.user = "test-user"
	s.client.setUnsignedTerms([]terms.GetTermsResponse{{
		Name:     "test-term",
		Revision: 1,
		Content:  testTerms,
	}})
	tests := []struct {
		about    string
		args     []string
		err      string
		stdout   string
		answer   string
		apiCalls []jujutesting.StubCall
	}{{
		about:    "everything works",
		args:     []string{"test-term/1", "--yes"},
		stdout:   "Agreed to revision 1 of test-term for Juju users\n",
		apiCalls: []jujutesting.StubCall{{FuncName: "SaveAgreement", Args: []interface{}{&terms.SaveAgreements{Agreements: []terms.SaveAgreement{{TermName: "test-term", TermRevision: 1}}}}}},
	}, {
		about: "cannot parse revision number",
		args:  []string{"test-term/abc"},
		err:   "invalid term format: strconv.ParseInt: parsing \"abc\": invalid syntax",
	}, {
		about: "missing arguments",
		args:  []string{},
		err:   "missing arguments",
	}, {
		about:  "everything works - user accepts",
		args:   []string{"test-term/1"},
		answer: "y",
		stdout: `
=== test-term/1: 0001-01-01 00:00:00 +0000 UTC ===
Test Terms
========
Do you agree to the displayed terms? (Y/n): Agreed to revision 1 of test-term for Juju users
`,
		apiCalls: []jujutesting.StubCall{{
			FuncName: "GetUnunsignedTerms", Args: []interface{}{
				&terms.CheckAgreementsRequest{Terms: []string{"test-term/1"}},
			},
		}, {
			FuncName: "SaveAgreement", Args: []interface{}{
				&terms.SaveAgreements{Agreements: []terms.SaveAgreement{{TermName: "test-term", TermRevision: 1}}},
			},
		}},
	}, {
		about:  "everything works - user refuses",
		args:   []string{"test-term/1"},
		answer: "n",
		stdout: `
=== test-term/1: 0001-01-01 00:00:00 +0000 UTC ===
Test Terms
========
Do you agree to the displayed terms? (Y/n): You didn't agree to the presented terms.
`,
		apiCalls: []jujutesting.StubCall{{
			FuncName: "GetUnunsignedTerms", Args: []interface{}{
				&terms.CheckAgreementsRequest{Terms: []string{"test-term/1"}},
			},
		}},
	}, {
		about: "must not accept 0 revision",
		args:  []string{"test-term/0", "--yes"},
		err:   `must specify a valid term revision "test-term/0"`,
	}, {
		about:  "user accepts, multiple terms",
		args:   []string{"test-term/1", "test-term/2"},
		answer: "y",
		stdout: `
=== test-term/1: 0001-01-01 00:00:00 +0000 UTC ===
Test Terms
========
Do you agree to the displayed terms? (Y/n): Agreed to revision 1 of test-term for Juju users
`,
		apiCalls: []jujutesting.StubCall{
			{
				FuncName: "GetUnunsignedTerms", Args: []interface{}{
					&terms.CheckAgreementsRequest{Terms: []string{"test-term/1", "test-term/2"}},
				},
			}, {
				FuncName: "SaveAgreement", Args: []interface{}{
					&terms.SaveAgreements{Agreements: []terms.SaveAgreement{
						{TermName: "test-term", TermRevision: 1},
					}},
				},
			}},
	}, {
		about: "valid then unknown arguments",
		args:  []string{"test-term/1", "unknown", "arguments"},
		err:   `must specify a valid term revision "unknown"`,
	}, {
		about: "user accepts all the terms",
		args:  []string{"test-term/1", "test-term/2", "--yes"},
		stdout: `Agreed to revision 1 of test-term for Juju users
Agreed to revision 2 of test-term for Juju users
`,
		apiCalls: []jujutesting.StubCall{
			{FuncName: "SaveAgreement", Args: []interface{}{&terms.SaveAgreements{
				Agreements: []terms.SaveAgreement{
					{TermName: "test-term", TermRevision: 1},
					{TermName: "test-term", TermRevision: 2},
				}}}}},
	},
	}
	for i, test := range tests {
		s.client.ResetCalls()
		c.Logf("running test %d: %s", i, test.about)
		if test.answer != "" {
			answer = test.answer
		}
		ctx, err := cmdtesting.RunCommand(c, agree.NewAgreeCommand(), test.args...)
		if test.err != "" {
			c.Assert(err, gc.ErrorMatches, test.err)
		} else {
			c.Assert(err, jc.ErrorIsNil)
		}
		if ctx != nil {
			c.Assert(cmdtesting.Stdout(ctx), gc.Equals, test.stdout)
		}
		if len(test.apiCalls) > 0 {
			s.client.CheckCalls(c, test.apiCalls)
		}
	}
}

type mockClient struct {
	jujutesting.Stub

	lock          sync.Mutex
	user          string
	terms         []terms.GetTermsResponse
	unsignedTerms []terms.GetTermsResponse
}

func (c *mockClient) setUnsignedTerms(t []terms.GetTermsResponse) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.unsignedTerms = t
}

// SaveAgreement saves user's agreement to the specified
// revision of the terms documents
func (c *mockClient) SaveAgreement(p *terms.SaveAgreements) (*terms.SaveAgreementResponses, error) {
	c.AddCall("SaveAgreement", p)
	responses := make([]terms.AgreementResponse, len(p.Agreements))
	for i, agreement := range p.Agreements {
		responses[i] = terms.AgreementResponse{
			User:     c.user,
			Term:     agreement.TermName,
			Revision: agreement.TermRevision,
		}
	}
	return &terms.SaveAgreementResponses{responses}, nil
}

func (c *mockClient) GetUnsignedTerms(p *terms.CheckAgreementsRequest) ([]terms.GetTermsResponse, error) {
	c.MethodCall(c, "GetUnunsignedTerms", p)
	r := make([]terms.GetTermsResponse, len(c.unsignedTerms))
	copy(r, c.unsignedTerms)
	return r, nil
}
