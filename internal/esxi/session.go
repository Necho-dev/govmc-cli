package esxi

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/fault"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/session/keepalive"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

const sessionKeepAlive = 5 * time.Minute

type authRetry struct {
	rt    soap.RoundTripper
	mu    sync.Mutex
	login func(context.Context) error
}

func (a *authRetry) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	err := a.rt.RoundTrip(ctx, req, res)
	if err == nil || !isNotAuthenticated(err) {
		return err
	}
	switch req.(type) {
	case *methods.LoginBody:
		return err
	}
	a.mu.Lock()
	loginErr := a.login(ctx)
	a.mu.Unlock()
	if loginErr != nil {
		log.Printf("esxi re-login failed: %v", loginErr)
		return err
	}
	log.Printf("esxi SOAP session re-authenticated after NotAuthenticated")
	return a.rt.RoundTrip(ctx, req, res)
}

func isNotAuthenticated(err error) bool {
	if err == nil {
		return false
	}
	if fault.Is(err, &types.NotAuthenticated{}) {
		return true
	}
	if soap.IsSoapFault(err) {
		if f := soap.ToSoapFault(err); f != nil {
			if _, ok := f.VimFault().(types.NotAuthenticated); ok {
				return true
			}
		}
	}
	return false
}

func Dial(ctx context.Context, esxiURL, user, pass string, insecure bool) (*Client, error) {
	u, err := soap.ParseURL(esxiURL)
	if err != nil {
		return nil, err
	}
	u.User = url.UserPassword(user, pass)

	soapClient := soap.NewClient(u, insecure)
	vimClient, err := vim25.NewClient(ctx, soapClient)
	if err != nil {
		soapClient.CloseIdleConnections()
		return nil, fmt.Errorf("connect to ESXi SDK: %w", err)
	}
	sm := session.NewManager(vimClient)
	login := func(ctx context.Context) error {
		return sm.Login(ctx, u.User)
	}
	retry := &authRetry{rt: vimClient.RoundTripper, login: login}
	vimClient.RoundTripper = keepalive.NewHandlerSOAP(retry, sessionKeepAlive, nil)

	c := &govmomi.Client{Client: vimClient, SessionManager: sm}
	if err := login(ctx); err != nil {
		c.Client.CloseIdleConnections()
		return nil, fmt.Errorf("authenticate ESXi: %w", err)
	}

	f := find.NewFinder(c.Client, true)
	dc, err := f.DefaultDatacenter(ctx)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.Logout(cleanup)
		return nil, fmt.Errorf("ESXi login succeeded, but datacenter discovery failed: %w", wrapVim(err))
	}
	f.SetDatacenter(dc)
	return &Client{
		Gov:    c,
		Finder: f,
		Perf:   performance.NewManager(c.Client),
		Reach:  u,
	}, nil
}
