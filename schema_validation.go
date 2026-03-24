package main

import (
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/schema"
)

var validator *schema.Validator

const schemaURL = "https://raw.githubusercontent.com/nostr-protocol/registry-of-kinds/refs/heads/master/schema.yaml"

func setSchemaValidator(enabled bool) error {
	if !enabled {
		validator = nil
		return nil
	}

	v, err := schema.NewValidatorFromURL(schemaURL)
	if err != nil {
		return err
	}
	validator = &v

	go func() {
		for {
			time.Sleep(time.Hour * 24)

			if validator == nil {
				return
			}

			newValidator, err := schema.NewValidatorFromURL(schemaURL)
			if err == nil {
				validator = &newValidator
			}
		}
	}()

	return nil
}

func validateSchema(event nostr.Event) error {
	if validator == nil {
		return nil
	}
	return validator.ValidateEvent(event)
}
