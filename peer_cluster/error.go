package peer_cluster

import "errors"

var (
	errNoBackupExist     = errors.New("no backup exist for disaster recovery")
	errInvalidMemberName = errors.New("the format of member's name is invalid")

	errUnexpectedUnreadyMember = errors.New("unexpected unready member for selfhosted cluster")

	errCreatedCluster = errors.New("cluster failed to be created")
)

func isFatalError(err error) bool {
	switch err {
	case errNoBackupExist, errInvalidMemberName, errUnexpectedUnreadyMember:
		return true
	default:
		return false
	}
}
