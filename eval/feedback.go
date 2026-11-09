package eval

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/logger"
	apipb "github.com/jsannemo/omogenexec/api"
)

// readPrefix reads at most the first limit bytes of the file at path. The returned bool tells
// whether the file contained more than limit bytes.
func readPrefix(path string, limit int) ([]byte, bool, error) {
	if limit < 0 {
		limit = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	// Read one byte extra to find out if there was more to read.
	data, err := ioutil.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// caseFeedback collects feedback about the run exit of the submission on the test case tc, the
// output of which is stored in tcPath.
//
// Feedback is best effort: problems with reading the output are logged rather than returned, so
// that they can never fail an evaluation.
func caseFeedback(tc *apipb.TestCase, tcPath string, exit *execResult) *apipb.CaseFeedback {
	feedback := &apipb.CaseFeedback{
		CaseName: tc.Name,
	}
	limit := int(tc.FeedbackLimitBytes)
	output, truncated, err := readPrefix(filepath.Join(tcPath, "output"), limit)
	if err != nil {
		logger.Warningf("could not read output of test case %s for feedback: %v", tc.Name, err)
	}
	feedback.TeamOutput = output
	feedback.TeamOutputTruncated = truncated

	errOutput, truncated, err := readPrefix(filepath.Join(tcPath, "error"), limit)
	if err != nil {
		logger.Warningf("could not read error output of test case %s for feedback: %v", tc.Name, err)
	}
	feedback.TeamError = errOutput
	feedback.TeamErrorTruncated = truncated

	switch exit.ExitType {
	case exited:
		feedback.ExitCode = int32(exit.ExitCode)
	case signaled:
		feedback.Signal = int32(exit.Signal)
	}
	return feedback
}
