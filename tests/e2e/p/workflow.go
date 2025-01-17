// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"errors"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// PChainWorkflow is an integration test for normal P-Chain operations
// - Issues an Add Validator and an Add Delegator using the funding address
// - Exports AVAX from the P-Chain funding address to the X-Chain created address
// - Exports AVAX from the X-Chain created address to the P-Chain created address
// - Checks the expected value of the funding address

var _ = e2e.DescribePChain("[Workflow]", func() {
	ginkgo.It("P-chain main operations",
		// use this for filtering tests by labels
		// ref. https://onsi.github.io/ginkgo/#spec-labels
		ginkgo.Label(
			"xp",
			"workflow",
		),
		ginkgo.FlakeAttempts(2),
		func() {
			nodeURI := e2e.Env.GetRandomNodeURI()
			keychain := e2e.Env.NewKeychain(2)
			baseWallet := e2e.Env.NewWallet(keychain, nodeURI)

			pWallet := baseWallet.P()
			avaxAssetID := baseWallet.P().AVAXAssetID()
			xWallet := baseWallet.X()
			pChainClient := platformvm.NewClient(nodeURI.URI)

			tests.Outf("{{blue}} fetching minimal stake amounts {{/}}\n")
			ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
			minValStake, minDelStake, err := pChainClient.GetMinStake(ctx, constants.PlatformChainID)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())
			tests.Outf("{{green}} minimal validator stake: %d {{/}}\n", minValStake)
			tests.Outf("{{green}} minimal delegator stake: %d {{/}}\n", minDelStake)

			tests.Outf("{{blue}} fetching tx fee {{/}}\n")
			infoClient := info.NewClient(nodeURI.URI)
			ctx, cancel = context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
			fees, err := infoClient.GetTxFee(ctx)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())
			txFees := uint64(fees.TxFee)
			tests.Outf("{{green}} txFee: %d {{/}}\n", txFees)

			// amount to transfer from P to X chain
			toTransfer := 1 * units.Avax

			pShortAddr := keychain.Keys[0].Address()
			xTargetAddr := keychain.Keys[1].Address()
			ginkgo.By("check selected keys have sufficient funds", func() {
				pBalances, err := pWallet.Builder().GetBalance()
				pBalance := pBalances[avaxAssetID]
				minBalance := minValStake + txFees + minDelStake + txFees + toTransfer + txFees
				gomega.Expect(pBalance, err).To(gomega.BeNumerically(">=", minBalance))
			})
			// create validator data
			validatorStartTimeDiff := 30 * time.Second
			vdrStartTime := time.Now().Add(validatorStartTimeDiff)

			// Use a random node ID to ensure that repeated test runs
			// will succeed against a persistent network.
			validatorID, err := ids.ToNodeID(utils.RandomBytes(ids.NodeIDLen))
			gomega.Expect(err).Should(gomega.BeNil())

			vdr := &txs.Validator{
				NodeID: validatorID,
				Start:  uint64(vdrStartTime.Unix()),
				End:    uint64(vdrStartTime.Add(72 * time.Hour).Unix()),
				Wght:   minValStake,
			}
			rewardOwner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{pShortAddr},
			}
			shares := uint32(20000) // TODO: retrieve programmatically

			ginkgo.By("issue add validator tx", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := pWallet.IssueAddValidatorTx(
					vdr,
					rewardOwner,
					shares,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			ginkgo.By("issue add delegator tx", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := pWallet.IssueAddDelegatorTx(
					vdr,
					rewardOwner,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			// retrieve initial balances
			pBalances, err := pWallet.Builder().GetBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			pStartBalance := pBalances[avaxAssetID]
			tests.Outf("{{blue}} P-chain balance before P->X export: %d {{/}}\n", pStartBalance)

			xBalances, err := xWallet.Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			xStartBalance := xBalances[avaxAssetID]
			tests.Outf("{{blue}} X-chain balance before P->X export: %d {{/}}\n", xStartBalance)

			outputOwner := secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					xTargetAddr,
				},
			}
			output := &secp256k1fx.TransferOutput{
				Amt:          toTransfer,
				OutputOwners: outputOwner,
			}

			ginkgo.By("export avax from P to X chain", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := pWallet.IssueExportTx(
					xWallet.BlockchainID(),
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: output,
						},
					},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			// check balances post export
			pBalances, err = pWallet.Builder().GetBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			pPreImportBalance := pBalances[avaxAssetID]
			tests.Outf("{{blue}} P-chain balance after P->X export: %d {{/}}\n", pPreImportBalance)

			xBalances, err = xWallet.Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			xPreImportBalance := xBalances[avaxAssetID]
			tests.Outf("{{blue}} X-chain balance after P->X export: %d {{/}}\n", xPreImportBalance)

			gomega.Expect(xPreImportBalance).To(gomega.Equal(xStartBalance)) // import not performed yet
			gomega.Expect(pPreImportBalance).To(gomega.Equal(pStartBalance - toTransfer - txFees))

			ginkgo.By("import avax from P into X chain", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := xWallet.IssueImportTx(
					constants.PlatformChainID,
					&outputOwner,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil(), "is context.DeadlineExceeded: %v", errors.Is(err, context.DeadlineExceeded))
			})

			// check balances post import
			pBalances, err = pWallet.Builder().GetBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			pFinalBalance := pBalances[avaxAssetID]
			tests.Outf("{{blue}} P-chain balance after P->X import: %d {{/}}\n", pFinalBalance)

			xBalances, err = xWallet.Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			xFinalBalance := xBalances[avaxAssetID]
			tests.Outf("{{blue}} X-chain balance after P->X import: %d {{/}}\n", xFinalBalance)

			gomega.Expect(xFinalBalance).To(gomega.Equal(xPreImportBalance + toTransfer - txFees)) // import not performed yet
			gomega.Expect(pFinalBalance).To(gomega.Equal(pPreImportBalance))
		})
})
