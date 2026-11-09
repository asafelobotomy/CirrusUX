package githost

import (
	"context"
	"io"
)

// HTTPBackend runs `git http-backend` over the repositories, as CGI: env
// holds the CGI variables (REQUEST_METHOD, PATH_INFO and so on), stdin
// the request body, and the CGI reply goes to stdout. Only the
// environment given here and Host's own reach git.
func (h *Host) HTTPBackend(ctx context.Context, env []string, stdin io.Reader, stdout io.Writer) error {
	cmd := append([]string{"GIT_PROJECT_ROOT=" + h.Dir, "GIT_HTTP_EXPORT_ALL=1"}, env...)
	return h.execIn(ctx, h.Dir, stdin, stdout, cmd, "http-backend")
}
