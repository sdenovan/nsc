/*
 * Copyright 2018 The NATS Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nats-io/jwt"
	"github.com/nats-io/nkeys"
	"github.com/nats-io/nsc/cmd/store"
	"github.com/spf13/cobra"
)

func createAddUserCmd() *cobra.Command {
	var params AddUserParams
	cmd := &cobra.Command{
		Use:          "user",
		Short:        "Add an user to the account",
		SilenceUsage: true,
		Example: `nsc add user -i
nsc add user --name u --deny-pubsub "bar.>"
nsc add user --name u --tag test,service_a`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := RunAction(cmd, args, &params); err != nil {
				return err
			}

			if params.generated {
				cmd.Printf("Generated user key - private key stored %q\n", params.keyPath)
			}
			cmd.Printf("Success! - added user %q to %q\n", params.name, params.accountName)

			return nil
		},
	}

	cmd.Flags().StringVarP(&params.accountName, "account", "a", "", "account name")

	cmd.Flags().StringSliceVarP(&params.allowPubs, "allow-pub", "", nil, "publish permissions - comma separated list or option can be specified multiple times")
	cmd.Flags().StringSliceVarP(&params.allowPubsub, "allow-pubsub", "", nil, "publish and subscribe permissions - comma separated list or option can be specified multiple times")
	cmd.Flags().StringSliceVarP(&params.allowSubs, "allow-sub", "", nil, "subscribe permissions - comma separated list or option can be specified multiple times")

	cmd.Flags().StringSliceVarP(&params.denyPubs, "deny-pub", "", nil, "deny publish permissions - comma separated list or option can be specified multiple times")
	cmd.Flags().StringSliceVarP(&params.denyPubsub, "deny-pubsub", "", nil, "deny publish and subscribe permissions - comma separated list or option can be specified multiple times")
	cmd.Flags().StringSliceVarP(&params.denySubs, "deny-sub", "", nil, "deny subscribe permissions - comma separated list or option can be specified multiple times")

	cmd.Flags().StringSliceVarP(&params.tags, "tag", "", nil, "tags for user - comma separated list or option can be specified multiple times")
	cmd.Flags().StringSliceVarP(&params.src, "source-network", "", nil, "source network for connection - comma separated list or option can be specified multiple times")

	cmd.Flags().StringVarP(&params.name, "name", "n", "", "name to assign the user")
	cmd.Flags().StringVarP(&params.keyPath, "public-key", "k", "", "public key identifying the user")

	params.TimeParams.BindFlags(cmd)

	return cmd
}

func init() {
	addCmd.AddCommand(createAddUserCmd())
}

type AddUserParams struct {
	accountKP   nkeys.KeyPair
	accountName string
	allowPubs   []string
	allowPubsub []string
	allowSubs   []string
	denyPubs    []string
	denyPubsub  []string
	denySubs    []string
	Entity
	src  []string
	tags []string
	TimeParams
}

func (p *AddUserParams) SetDefaults(ctx ActionCtx) error {
	p.create = true
	p.kind = nkeys.PrefixByteUser
	p.editFn = p.editUserClaim

	if p.accountName == "" {
		p.accountName = ctx.StoreCtx().Account.Name
	}

	return nil
}

func (p *AddUserParams) PreInteractive(ctx ActionCtx) error {
	var err error
	if err = p.Entity.Edit(); err != nil {
		return err
	}

	p.accountName, err = ctx.StoreCtx().PickAccount(p.accountName)
	if err != nil {
		return err
	}

	if err = p.TimeParams.Edit(); err != nil {
		return err
	}

	p.accountKP, err = ctx.StoreCtx().ResolveKey(nkeys.PrefixByteAccount, KeyPathFlag)
	if err != nil {
		return err
	}

	if p.accountKP == nil {
		err = EditKeyPath(nkeys.PrefixByteAccount, "account keypath", &KeyPathFlag)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *AddUserParams) Load(ctx ActionCtx) error {
	return nil
}

func (p *AddUserParams) PostInteractive(ctx ActionCtx) error {
	return nil
}

func (p *AddUserParams) Validate(ctx ActionCtx) error {
	var err error
	if p.name == "" {
		ctx.CurrentCmd().SilenceUsage = false
		return fmt.Errorf("user name is required")
	}

	if p.accountName == "" {
		// default account was not found by get context, so we either we have none or many
		accounts, err := ctx.StoreCtx().Store.ListSubContainers(store.Accounts)
		if err != nil {
			return err
		}
		c := len(accounts)
		if c == 0 {
			return errors.New("no accounts defined - add account first")
		} else {
			return errors.New("multiple accounts found - specify --account-name or navigate to an account directory")
		}
	}

	p.accountKP, err = ctx.StoreCtx().ResolveKey(nkeys.PrefixByteAccount, KeyPathFlag)
	if err != nil {
		return err
	}

	if err := p.TimeParams.Validate(); err != nil {
		return err
	}

	return p.Entity.Valid()
}

func (p *AddUserParams) Run(ctx ActionCtx) error {
	if err := p.Entity.StoreKeys(p.accountName); err != nil {
		return err
	}

	if err := p.Entity.GenerateClaim(p.accountKP); err != nil {
		return err
	}

	return nil
}

func (p *AddUserParams) editUserClaim(c interface{}) error {
	uc, ok := c.(*jwt.UserClaims)
	if !ok {
		return errors.New("unable to cast to user claim")
	}

	if p.TimeParams.IsStartChanged() {
		uc.NotBefore, _ = p.TimeParams.StartDate()
	}

	if p.TimeParams.IsExpiryChanged() {
		uc.Expires, _ = p.TimeParams.ExpiryDate()
	}

	uc.Permissions.Pub.Allow.Add(p.allowPubs...)
	uc.Permissions.Pub.Allow.Add(p.allowPubsub...)
	sort.Strings(uc.Pub.Allow)

	uc.Permissions.Pub.Deny.Add(p.denyPubs...)
	uc.Permissions.Pub.Deny.Add(p.denyPubsub...)
	sort.Strings(uc.Permissions.Pub.Deny)

	uc.Permissions.Sub.Allow.Add(p.allowSubs...)
	uc.Permissions.Sub.Allow.Add(p.allowPubsub...)
	sort.Strings(uc.Permissions.Sub.Allow)

	uc.Permissions.Sub.Deny.Add(p.denySubs...)
	uc.Permissions.Sub.Deny.Add(p.denyPubsub...)
	sort.Strings(uc.Permissions.Sub.Deny)

	uc.Tags.Add(p.tags...)
	sort.Strings(uc.Tags)

	return nil
}
