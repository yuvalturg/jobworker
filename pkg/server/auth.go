package server

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// This holds a map of { jobID -> owner (clientName) }
type authHandler struct {
	jobClientMap sync.Map
}

func newAuthHandler() *authHandler {
	return &authHandler{}
}

// startJobAllowed:
// - Tries to get the client's commonName from its certificate
// - If no clientName is provided, returns a PermissionDenied
func (h *authHandler) startJobAllowed(ctx context.Context) (string, error) {
	clientName, err := getClientCommonName(ctx)
	if err != nil {
		return "", err
	}

	if clientName == "" {
		return "", status.Errorf(codes.PermissionDenied, "missing client name")
	}

	return clientName, nil
}

// registerJobID:
// - Registers a newly created jobID with its owner
func (h *authHandler) registerJobID(jobID, owner string) {
	h.jobClientMap.Store(jobID, owner)
}

// checkOwnership:
// - Makes sure a job that is being accessed is owned by the calling client
func (h *authHandler) checkOwnership(ctx context.Context, jobId string) error {
	clientName, err := getClientCommonName(ctx)
	if err != nil {
		return err
	}

	if owner, ok := h.jobClientMap.Load(jobId); ok {
		if owner == clientName {
			return nil
		}
	}

	return status.Errorf(codes.PermissionDenied, "%s cannot access job %s", clientName, jobId)
}

// getClientCommonName:
// - Each certificate should hold a `Subject: CN = <name>`.
// - Extracts the client common name from the peer certificate.
// - If the client name could not be extracted, fail on PermissionDenied
func getClientCommonName(ctx context.Context) (string, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Errorf(codes.PermissionDenied, "failed to get peer from context")
	}

	tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", status.Errorf(codes.PermissionDenied, "failed to get TLSInfo from peer")
	}

	if len(tlsInfo.State.PeerCertificates) == 0 {
		return "", status.Errorf(codes.PermissionDenied, "no peer certificates found")
	}

	peerCert := tlsInfo.State.PeerCertificates[0]
	return peerCert.Subject.CommonName, nil
}
