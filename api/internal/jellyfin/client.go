// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golusoris/goenvoy/mediaserver/jellyfin/v3"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Client implements Authenticator on top of goenvoy jellyfin v3.
type Client struct {
	baseURL    string
	timeout    time.Duration
	userAgent  string
	httpClient *http.Client
	svcClient  *jellyfin.Client
}

// NewClient creates a new Jellyfin Authenticator client.
func NewClient(baseURL, apiKey string, timeout time.Duration, userAgent string, httpClient *http.Client) (*Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	opts := []jellyfin.Option{
		jellyfin.WithAccessToken(apiKey),
		jellyfin.WithTimeout(timeout),
		jellyfin.WithUserAgent(userAgent),
	}
	if httpClient != nil {
		clCopy := *httpClient
		opts = append(opts, jellyfin.WithHTTPClient(&clCopy))
	}
	svc, err := jellyfin.New(baseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("jellyfin: create service client: %w", err)
	}
	return &Client{
		baseURL:    baseURL,
		timeout:    timeout,
		userAgent:  userAgent,
		httpClient: httpClient,
		svcClient:  svc,
	}, nil
}

func (c *Client) newFreshClient() (*jellyfin.Client, error) {
	opts := []jellyfin.Option{
		jellyfin.WithTimeout(c.timeout),
		jellyfin.WithUserAgent(c.userAgent),
	}
	if c.httpClient != nil {
		clCopy := *c.httpClient
		opts = append(opts, jellyfin.WithHTTPClient(&clCopy))
	}
	cl, err := jellyfin.New(c.baseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("jellyfin: create fresh client: %w", err)
	}
	return cl, nil
}

func mapAPIError(err error, map404ToUnauthorized bool) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := errors.AsType[*jellyfin.APIError](err); ok {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: %s", domain.ErrUnauthorized, apiErr.Error())
		}
		if map404ToUnauthorized && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", domain.ErrUnauthorized, apiErr.Error())
		}
	}
	return fmt.Errorf("jellyfin: %w", err)
}

func toMember(u *jellyfin.UserDto) (Member, error) {
	if u == nil || u.Policy == nil {
		return Member{}, fmt.Errorf("%w: missing user policy", domain.ErrUnauthorized)
	}
	return Member{
		ID:       u.ID,
		Name:     u.Name,
		Admin:    u.Policy.IsAdministrator,
		Disabled: u.Policy.IsDisabled,
		Policy: Policy{
			MaxParentalRating: u.Policy.MaxParentalRating,
			BlockedTags:       u.Policy.BlockedTags,
			AllowedTags:       u.Policy.AllowedTags,
			EnableAllFolders:  u.Policy.EnableAllFolders,
			EnabledFolders:    u.Policy.EnabledFolders,
		},
	}, nil
}

// SignIn verifies username and password, dropping the access token and returning the Member.
func (c *Client) SignIn(ctx context.Context, user, pw string) (Member, error) {
	cl, err := c.newFreshClient()
	if err != nil {
		return Member{}, fmt.Errorf("jellyfin: new fresh client: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res, err := cl.AuthenticateUserByName(reqCtx, user, pw)
	if err != nil {
		return Member{}, mapAPIError(err, false)
	}
	return toMember(res.User)
}

// QuickConnectEnabled checks if Quick Connect is enabled on the server.
func (c *Client) QuickConnectEnabled(ctx context.Context) (bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	enabled, err := c.svcClient.QuickConnectEnabled(reqCtx)
	if err != nil {
		return false, mapAPIError(err, false)
	}
	return enabled, nil
}

// InitiateQuickConnect starts a Quick Connect pairing flow.
func (c *Client) InitiateQuickConnect(ctx context.Context) (string, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res, err := c.svcClient.InitiateQuickConnect(reqCtx)
	if err != nil {
		return "", "", mapAPIError(err, false)
	}
	return res.Code, res.Secret, nil
}

// QuickConnectAuthorized checks if the secret has been authorized.
func (c *Client) QuickConnectAuthorized(ctx context.Context, secret string) (bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res, err := c.svcClient.GetQuickConnectState(reqCtx, secret)
	if err != nil {
		return false, mapAPIError(err, true)
	}
	return res.Authenticated, nil
}

// SignInQuickConnect exchanges an authorized secret for a user session, returning the Member.
func (c *Client) SignInQuickConnect(ctx context.Context, secret string) (Member, error) {
	cl, err := c.newFreshClient()
	if err != nil {
		return Member{}, fmt.Errorf("jellyfin: new fresh client: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res, err := cl.AuthenticateWithQuickConnect(reqCtx, secret)
	if err != nil {
		return Member{}, mapAPIError(err, true)
	}
	return toMember(res.User)
}

// User fetches a user by id using the service client.
func (c *Client) User(ctx context.Context, id string) (Member, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	u, err := c.svcClient.GetUser(reqCtx, id)
	if err != nil {
		return Member{}, mapAPIError(err, false)
	}
	return toMember(u)
}
