package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nip86"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func allowPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	log.Info().Str("caller", caller.Hex()).Str("pubkey", pubkey.Hex()).Str("reason", reason).Msg("management allowpubkey called")

	err := pyramid.AddAction("invite", caller, pubkey)
	if err == nil {
		publishMembershipChange(pubkey, true)
	}
	return err
}

func banPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	log.Info().Str("caller", caller.Hex()).Str("pubkey", pubkey.Hex()).Str("reason", reason).Msg("management banpubkey called")

	err := pyramid.AddAction("drop", caller, pubkey)
	if err == nil {
		publishMembershipChange(pubkey, false)
	}
	return err
}

func listAllowedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	log.Info().Msg("management listallowedpubkeys called")
	list := make([]nip86.PubKeyReason, 0, pyramid.Members.Size())
	for pubkey, member := range pyramid.Members.Range {
		if len(member.Parents) == 0 || member.Removed {
			continue
		}
		reason := "invited by "
		for j, inv := range member.Parents {
			if j > 0 {
				reason += ", "
			}
			if inv == pyramid.AbsoluteKey {
				reason += "root"
			} else {
				reason += "nostr:" + nip19.EncodeNpub(inv)
			}
		}
		list = append(list, nip86.PubKeyReason{PubKey: pubkey, Reason: reason})
	}
	return list, nil
}

func listBannedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	log.Info().Msg("management listbannedpubkeys called")
	list := make([]nip86.PubKeyReason, 0, pyramid.Members.Size())
	for pubkey, member := range pyramid.Members.Range {
		if !member.Removed {
			continue
		}
		reason := "removed member"
		list = append(list, nip86.PubKeyReason{PubKey: pubkey, Reason: reason})
	}
	return list, nil
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	// allow if caller is a root user
	if pyramid.IsRoot(caller) {
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("management banevent called by root")
	} else {
		// check if the caller is the author of the event being banned
		var isAuthor bool
		for evt := range global.IL.Main.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if evt.PubKey == caller {
				isAuthor = true
				break
			}
		}
		if !isAuthor {
			return fmt.Errorf("must be a root user or the event author to ban an event")
		}
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("management banevent called by author")
	}

	var deleted nostr.Event
	for evt := range global.IL.Main.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		deleted = evt
	}

	if deleted.PubKey != nostr.ZeroPK {
		if err := deleteFromMain(id); err != nil {
			return err
		}

		handleDeleted(ctx, deleted)
	}

	return nil
}

func changeRelayNameHandler(ctx context.Context, name string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Str("name", name).Msg("management changerelayname called")

	global.Settings.RelayName = name
	return global.SaveUserSettings()
}

func changeRelayDescriptionHandler(ctx context.Context, description string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Str("description", description).Msg("management changerelaydescription called")

	global.Settings.RelayDescription = description
	return global.SaveUserSettings()
}

func changeRelayIconHandler(ctx context.Context, icon string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Str("icon", icon).Msg("management changerelayicon called")

	global.Settings.RelayIcon = icon
	return global.SaveUserSettings()
}

func allowKindHandler(ctx context.Context, kind nostr.Kind) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Uint16("kind", uint16(kind)).Msg("management allowkind called")

	if global.Settings.AllowedKindsSpec == "all" {
		return fmt.Errorf("all kinds are supported already")
	}

	list, err := global.ParseKinds(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)
	if err != nil {
		return err
	}

	if slices.Contains(list, kind) {
		return nil
	}

	if strings.Contains(global.Settings.AllowedKindsSpec, "+") || strings.Contains(global.Settings.AllowedKindsSpec, "-") || strings.TrimSpace(global.Settings.AllowedKindsSpec) == "" {
		// is delta
		global.Settings.AllowedKindsSpec += ",+" + strconv.Itoa(int(kind))
	} else {
		// is specific
		global.Settings.AllowedKindsSpec += "," + strconv.Itoa(int(kind))
	}

	// rebuild
	global.KindIsAllowed, _ = global.BuildKindIsAllowedFunction(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)

	return global.SaveUserSettings()
}

func listAllowedKindsHandler(ctx context.Context) ([]nostr.Kind, error) {
	if global.Settings.AllowedKindsSpec == "all" {
		return []nostr.Kind{}, nil
	} else {
		return global.ParseKinds(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)
	}
}

func disallowKindHandler(ctx context.Context, kind nostr.Kind) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Uint16("kind", uint16(kind)).Msg("management disallowkind called")

	if global.Settings.AllowedKindsSpec == "all" {
		return fmt.Errorf("all kinds are supported, must change that in the settings")
	}

	list, err := global.ParseKinds(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)
	if err != nil {
		return err
	}

	if !slices.Contains(list, kind) {
		return nil
	}

	if strings.Contains(global.Settings.AllowedKindsSpec, "+") || strings.Contains(global.Settings.AllowedKindsSpec, "-") || strings.TrimSpace(global.Settings.AllowedKindsSpec) == "" {
		// is delta
		global.Settings.AllowedKindsSpec += ",-" + strconv.Itoa(int(kind))
	} else {
		// is specific
		listStr := make([]string, 0, len(list))
		for _, ek := range list {
			if ek != kind {
				listStr = append(listStr, strconv.Itoa(int(ek)))
			}
		}
		global.Settings.AllowedKindsSpec = strings.Join(listStr, ",")
	}

	// rebuild this
	global.KindIsAllowed, _ = global.BuildKindIsAllowedFunction(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)

	return global.SaveUserSettings()
}
