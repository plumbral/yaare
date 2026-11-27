package ipc

import (
	"yaare/internal/domain/syncmodel"
	"yaare/internal/domain/workspace"
)

func preserveProxyCredentials(next *syncmodel.LocalData, current syncmodel.LocalData) {
	hosts := make(map[string]workspace.Host, len(next.Snapshot.Hosts))
	for _, host := range next.Snapshot.Hosts {
		hosts[host.ID] = host
	}
	for _, c := range current.ProxyCredentials {
		if workspace.ProxyCredentialMatches(hosts[c.HostID], c) {
			next.ProxyCredentials = append(next.ProxyCredentials, c)
		}
	}
}

func preserveHostCredentials(next *syncmodel.LocalData, current syncmodel.LocalData) {
	oldHosts := map[string]workspace.Host{}
	for _, h := range current.Snapshot.Hosts {
		oldHosts[h.ID] = h
	}
	identities := map[string]workspace.Identity{}
	for _, i := range current.Identities {
		identities[i.ID] = i.Identity
	}
	byID := map[string]workspace.HostCredential{}
	for _, c := range current.HostCredentials {
		byID[c.HostID] = c
	}
	next.HostCredentials = nil
	for i := range next.Snapshot.Hosts {
		h := &next.Snapshot.Hosts[i]
		h.HasCredential = false
		if old, ok := oldHosts[h.ID]; ok && old.Address == h.Address && old.Port == h.Port {
			if identity, ok := identities[old.IdentityID]; ok && identity.Username == h.Username && identity.Authentication == h.Authentication && identity.UseAgent == h.UseAgent {
				h.IdentityID = identity.ID
			} else if old.IdentityID == "" && old.Username == h.Username && old.Authentication == h.Authentication && old.UseAgent == h.UseAgent {
				h.KeyID = old.KeyID
			}
		}
		if c, ok := byID[h.ID]; ok && workspace.CredentialMatches(*h, c) {
			next.HostCredentials = append(next.HostCredentials, c)
			h.HasCredential = true
		}
	}
}
