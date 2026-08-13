package integrations

import "testing"

func TestImageLooksLikeBareTag(t *testing.T) {
	cases := []struct {
		image string
		want  bool
	}{
		// The exact reference that failed for a user: a version passed as image.
		{"18.11.9-ce.0", true},
		{"18.11.9-ce.0:latest", true},
		{"v1.2.3", true},
		{"1.9", true},
		// Valid full references must not be flagged.
		{"gitlab/gitlab-ce:18.11.9-ce.0", false},
		{"gitlab/gitlab-ce", false},
		{"registry.gitlab.com/gitlab-org/gitlab-ce:18.11.9-ce.0", false},
		{"nginx", false},
		{"nginx:alpine", false},
		{"redis:8-alpine", false},
	}

	for _, tc := range cases {
		if got := imageLooksLikeBareTag(tc.image); got != tc.want {
			t.Errorf("imageLooksLikeBareTag(%q) = %v, want %v", tc.image, got, tc.want)
		}
	}
}
