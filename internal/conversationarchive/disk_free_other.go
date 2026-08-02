//go:build !unix

package conversationarchive

func availableBytes(string) (int64, error) {
	// The storage reserve is enforced on supported Unix production hosts. Other
	// platforms retain the archive contract but report the capacity as unknown.
	return -1, nil
}
