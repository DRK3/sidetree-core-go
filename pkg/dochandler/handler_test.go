/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package dochandler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	batchapi "github.com/trustbloc/sidetree-core-go/pkg/api/batch"
	"github.com/trustbloc/sidetree-core-go/pkg/api/protocol"
	"github.com/trustbloc/sidetree-core-go/pkg/batch"
	"github.com/trustbloc/sidetree-core-go/pkg/batch/cutter"
	"github.com/trustbloc/sidetree-core-go/pkg/batch/opqueue"
	"github.com/trustbloc/sidetree-core-go/pkg/dochandler/didvalidator"
	"github.com/trustbloc/sidetree-core-go/pkg/dochandler/docvalidator"
	"github.com/trustbloc/sidetree-core-go/pkg/document"
	"github.com/trustbloc/sidetree-core-go/pkg/docutil"
	"github.com/trustbloc/sidetree-core-go/pkg/jws"
	"github.com/trustbloc/sidetree-core-go/pkg/mocks"
	"github.com/trustbloc/sidetree-core-go/pkg/patch"
	"github.com/trustbloc/sidetree-core-go/pkg/processor"
	"github.com/trustbloc/sidetree-core-go/pkg/restapi/model"
)

const (
	namespace = "doc:namespace"

	sha2_256           = 18
	initialValuesParam = ";initial-values="
)

func TestDocumentHandler_Namespace(t *testing.T) {
	dh := New(namespace, nil, nil, nil, nil)
	require.Equal(t, namespace, dh.Namespace())
}

func TestDocumentHandler_Protocol(t *testing.T) {
	pc := mocks.NewMockProtocolClient()
	dh := New("", pc, nil, nil, nil)
	require.Equal(t, pc, dh.Protocol())
}

func TestDocumentHandler_ProcessOperation_Create(t *testing.T) {
	dochandler := getDocumentHandler(mocks.NewMockOperationStore(nil))
	require.NotNil(t, dochandler)

	createOp := getCreateOperation()

	doc, err := dochandler.ProcessOperation(createOp)
	require.Nil(t, err)
	require.NotNil(t, doc)
}

func TestDocumentHandler_ProcessOperation_InitialDocumentError(t *testing.T) {
	dochandler := getDocumentHandler(mocks.NewMockOperationStore(nil))
	require.NotNil(t, dochandler)

	replacePatch, err := patch.NewReplacePatch("{}")
	require.NoError(t, err)
	replacePatch["document"] = "invalid"

	createOp := getCreateOperation()

	createOp.PatchData = &model.PatchDataModel{
		Patches: []patch.Patch{replacePatch},
	}

	doc, err := dochandler.ProcessOperation(createOp)
	require.NotNil(t, err)
	require.Nil(t, doc)
	require.Contains(t, err.Error(), "invalid character")
}

func TestDocumentHandler_ProcessOperation_MazOperationSizeError(t *testing.T) {
	dochandler := getDocumentHandler(mocks.NewMockOperationStore(nil))
	require.NotNil(t, dochandler)

	// modify handler protocol client to decrease max operation size
	protocol := mocks.NewMockProtocolClient()
	protocol.Protocol.MaxOperationByteSize = 2
	dochandler.protocol = protocol

	createOp := getCreateOperation()

	doc, err := dochandler.ProcessOperation(createOp)
	require.NotNil(t, err)
	require.Nil(t, doc)
	require.Contains(t, err.Error(), "operation byte size exceeds protocol max operation byte size")
}

func TestDocumentHandler_ResolveDocument_DID(t *testing.T) {
	store := mocks.NewMockOperationStore(nil)
	dochandler := getDocumentHandler(store)
	require.NotNil(t, dochandler)

	docID := getCreateOperation().ID

	// scenario: not found in the store
	result, err := dochandler.ResolveDocument(docID)
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "not found")

	// insert document in the store
	err = store.Put(getCreateOperation())
	require.Nil(t, err)

	// scenario: resolved document (success)
	result, err = dochandler.ResolveDocument(docID)
	require.Nil(t, err)
	require.NotNil(t, result)
	require.Equal(t, true, result.MethodMetadata.Published)

	// scenario: invalid namespace
	result, err = dochandler.ResolveDocument("doc:invalid:")
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "must start with configured namespace")

	// scenario: invalid id
	result, err = dochandler.ResolveDocument(namespace + docutil.NamespaceDelimiter)
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "unique portion is empty")
}

func TestDocumentHandler_ResolveDocument_InitialValue(t *testing.T) {
	dochandler := getDocumentHandler(mocks.NewMockOperationStore(nil))
	require.NotNil(t, dochandler)

	docID := getCreateOperation().ID

	encodedRequest := docutil.EncodeToString(getCreateOperation().OperationBuffer)

	result, err := dochandler.ResolveDocument(docID + initialValuesParam + encodedRequest)
	require.NotNil(t, result)
	require.Equal(t, false, result.MethodMetadata.Published)

	result, err = dochandler.ResolveDocument(docID + initialValuesParam)
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "initial values is present but empty")

	// create request not encoded
	result, err = dochandler.ResolveDocument(docID + initialValuesParam + "payload")
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "invalid character")

	// invalid create request - not json
	result, err = dochandler.ResolveDocument(docID + initialValuesParam + docutil.EncodeToString([]byte("payload")))
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "invalid character")

	// did doesn't match the one created by parsing original create request
	result, err = dochandler.ResolveDocument(dochandler.namespace + ":someID" + initialValuesParam + encodedRequest)
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "provided did doesn't match did created from create request")
}

func TestDocumentHandler_ResolveDocument_InitialValue_MaxOperationSizeError(t *testing.T) {
	dochandler := getDocumentHandler(mocks.NewMockOperationStore(nil))
	require.NotNil(t, dochandler)

	// modify handler protocol client to decrease max operation size
	protocol := mocks.NewMockProtocolClient()
	protocol.Protocol.MaxOperationByteSize = 2
	dochandler.protocol = protocol

	docID := getCreateOperation().ID

	result, err := dochandler.ResolveDocument(docID + initialValuesParam + docutil.EncodeToString(getCreateOperation().OperationBuffer))
	require.NotNil(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "operation byte size exceeds protocol max operation byte size")
}

func TestTransformToExternalDocument(t *testing.T) {
	dochandler := getDocumentHandler(nil)

	result, err := dochandler.transformToExternalDoc(nil, "abc")
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "internal document is nil")

	doc := document.Document{}
	result, err = dochandler.transformToExternalDoc(doc, "abc")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "abc", result.Document[keyID])
}

func TestGetUniquePortion(t *testing.T) {
	const namespace = "did:sidetree"

	// id doesn't contain namespace
	uniquePortion, err := getUniquePortion(namespace, "invalid")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "ID must start with configured namespace")

	// id equals namespace; unique portion is empty
	uniquePortion, err = getUniquePortion(namespace, namespace+docutil.NamespaceDelimiter)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "unique portion is empty")

	// valid unique portion
	const unique = "exKwW0HjS5y4zBtJ7vYDwglYhtckdO15JDt1j5F5Q0A"
	uniquePortion, err = getUniquePortion(namespace, namespace+docutil.NamespaceDelimiter+unique)
	require.Nil(t, err)
	require.Equal(t, unique, uniquePortion)
}

func TestGetParts(t *testing.T) {
	const testDID = "did:method:abc"

	did, initial, err := getParts(testDID)
	require.Nil(t, err)
	require.Empty(t, initial)
	require.Equal(t, testDID, did)

	did, initial, err = getParts(testDID + initialValuesParam)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "initial values is present but empty")

	did, initial, err = getParts(testDID + initialValuesParam + "xyz")
	require.Nil(t, err)
	require.Equal(t, testDID, did)
	require.Equal(t, initial, "xyz")
}

func TestProcessOperation_Update(t *testing.T) {
	store := mocks.NewMockOperationStore(nil)
	dochandler := getDocumentHandler(store)
	require.NotNil(t, dochandler)

	// insert document in the store
	err := store.Put(getCreateOperation())
	require.Nil(t, err)

	// modify default validator to did validator since update payload is did document update
	validator := didvalidator.New(store)
	dochandler.validator = validator

	doc, err := dochandler.ProcessOperation(getUpdateOperation())
	require.Nil(t, err)
	require.Nil(t, doc)
}

// BatchContext implements batch writer context
type BatchContext struct {
	ProtocolClient   *mocks.MockProtocolClient
	CasClient        *mocks.MockCasClient
	BlockchainClient *mocks.MockBlockchainClient
	OpQueue          cutter.OperationQueue
}

// Protocol returns the ProtocolClient
func (m *BatchContext) Protocol() protocol.Client {
	return m.ProtocolClient
}

// Blockchain returns the block chain client
func (m *BatchContext) Blockchain() batch.BlockchainClient {
	return m.BlockchainClient
}

// CAS returns the CAS client
func (m *BatchContext) CAS() batch.CASClient {
	return m.CasClient
}

// OperationQueue returns the queue of operations pending to be cut
func (m *BatchContext) OperationQueue() cutter.OperationQueue {
	return m.OpQueue
}

func getDocumentHandler(store processor.OperationStoreClient) *DocumentHandler {
	protocol := mocks.NewMockProtocolClient()

	validator := docvalidator.New(store)
	processor := processor.New("test", store)

	ctx := &BatchContext{
		ProtocolClient:   protocol,
		CasClient:        mocks.NewMockCasClient(nil),
		BlockchainClient: mocks.NewMockBlockchainClient(nil),
		OpQueue:          &opqueue.MemQueue{},
	}
	writer, err := batch.New("test", ctx)
	if err != nil {
		panic(err)
	}

	// start go routine for cutting batches
	writer.Start()

	return New(namespace, protocol, validator, writer, processor)
}

func getCreateOperation() *batchapi.Operation {
	request, err := getCreateRequest()
	if err != nil {
		panic(err)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}

	uniqueSuffix, err := docutil.CalculateUniqueSuffix(request.SuffixData, sha2_256)
	if err != nil {
		panic(err)
	}

	patchDataBytes, err := docutil.DecodeString(request.PatchData)
	if err != nil {
		panic(err)
	}

	patchData := &model.PatchDataModel{}
	err = json.Unmarshal(patchDataBytes, patchData)
	if err != nil {
		panic(err)
	}

	return &batchapi.Operation{
		OperationBuffer:              payload,
		PatchData:                    patchData,
		EncodedPatchData:             request.PatchData,
		Type:                         batchapi.OperationTypeCreate,
		HashAlgorithmInMultiHashCode: sha2_256,
		UniqueSuffix:                 uniqueSuffix,
		ID:                           namespace + docutil.NamespaceDelimiter + uniqueSuffix,
		SuffixData:                   getSuffixData(),
	}
}

const validDoc = `{
	"publicKey": [{
		"id": "key-1",
		"publicKeyBase58": "GY4GunSXBPBfhLCzDL7iGmP5dR3sBDCJZkkaGK8VgYQf",
		"type": "Ed25519VerificationKey2018"
	}]
}`

func getCreateRequest() (*model.CreateRequest, error) {
	patchData, err := getPatchData()
	if err != nil {
		return nil, err
	}

	patchDataBytes, err := json.Marshal(patchData)
	if err != nil {
		return nil, err
	}

	suffixDataBytes, err := docutil.MarshalCanonical(getSuffixData())
	if err != nil {
		return nil, err
	}

	return &model.CreateRequest{
		Operation:  model.OperationTypeCreate,
		PatchData:  docutil.EncodeToString(patchDataBytes),
		SuffixData: docutil.EncodeToString(suffixDataBytes),
	}, nil
}

func getPatchData() (*model.PatchDataModel, error) {
	replacePatch, err := patch.NewReplacePatch(validDoc)
	if err != nil {
		return nil, err
	}

	return &model.PatchDataModel{
		Patches:                  []patch.Patch{replacePatch},
		NextUpdateCommitmentHash: computeMultihash("updateReveal"),
	}, nil
}

func getSuffixData() *model.SuffixDataModel {
	return &model.SuffixDataModel{
		PatchDataHash:              computeMultihash(validDoc),
		RecoveryKey:                &jws.JWK{},
		NextRecoveryCommitmentHash: computeMultihash("recoveryReveal"),
	}
}

func computeMultihash(data string) string {
	mh, err := docutil.ComputeMultihash(sha2_256, []byte(data))
	if err != nil {
		panic(err)
	}
	return docutil.EncodeToString(mh)
}

func getUpdateRequest() (*model.UpdateRequest, error) {
	patchDataBytes, err := json.Marshal(getUpdatePatchData())
	if err != nil {
		return nil, err
	}

	return &model.UpdateRequest{
		Operation:       model.OperationTypeUpdate,
		DidUniqueSuffix: getCreateOperation().UniqueSuffix,
		PatchData:       docutil.EncodeToString(patchDataBytes),
	}, nil
}

func getUpdatePatchData() *model.PatchDataModel {
	return &model.PatchDataModel{
		NextUpdateCommitmentHash: computeMultihash("updateReveal"),
	}
}

func getUpdateOperation() *batchapi.Operation {
	request, err := getUpdateRequest()
	if err != nil {
		panic(err)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}

	return &batchapi.Operation{
		OperationBuffer:              payload,
		Type:                         batchapi.OperationTypeUpdate,
		HashAlgorithmInMultiHashCode: sha2_256,
		UniqueSuffix:                 request.DidUniqueSuffix,
		ID:                           namespace + docutil.NamespaceDelimiter + request.DidUniqueSuffix,
	}
}
