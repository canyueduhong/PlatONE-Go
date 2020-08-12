package packet

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/PlatONEnetwork/PlatONE-Go/accounts/abi"
	"github.com/PlatONEnetwork/PlatONE-Go/common"
	"github.com/PlatONEnetwork/PlatONE-Go/crypto"

	"github.com/PlatONEnetwork/PlatONE-Go/common/hexutil"
	"github.com/PlatONEnetwork/PlatONE-Go/rlp"

	"github.com/PlatONEnetwork/PlatONE-Go/cmd/platonecli/utils"
)

// MessageCallDemo, the interface for different types of data package methods
type MsgDataGen interface {
	CombineData() (string, []abi.ArgumentMarshaling, bool, error)
	ReceiptParsing(receipt *Receipt) string
	ParseNonConstantResponse(respStr string, outputType []abi.ArgumentMarshaling) []interface{}
	GetAbiBytes() []byte
}

type deployInter interface {
	combineData() (string, error)
	ReceiptParsing(*Receipt, []byte) string
}

type contractInter interface {
	encodeFuncName(string) []byte
	encodeFunction(*FuncDesc, []string, string) ([][]byte, error)
	combineData([][]byte) (string, error)
	setIsWrite(*FuncDesc) bool
	ReceiptParsing(*Receipt, []byte) string
	ParseNonConstantResponse(respStr string, outputType []abi.ArgumentMarshaling) []interface{}
}

//============================Contract EVM============================

type EvmContractInterpreter struct {
	typeName []string // contract parameter types
}

// deprecated
func tempStructConvert(input FuncIO) abi.ArgumentMarshaling {
	var output abi.ArgumentMarshaling
	output.Type = input.Type
	output.InternalType = input.InternalType
	output.Name = input.Name

	for _, v := range input.Components {
		output.Components = append(output.Components, tempStructConvert(v))
	}

	return output
}

// encodeFunction converts the function params to bytes and combine them by specific encoding rules
func (i *EvmContractInterpreter) encodeFunction(abiFunc *FuncDesc, funcParams []string, funcName string) ([][]byte, error) {
	var arguments abi.Arguments
	var funcByte = make([][]byte, 1)
	var paramTypes = make([]string, 0)
	var args = make([]interface{}, 0)
	var argument abi.Argument
	var err error

	// converts the function params to bytes
	for i, v := range funcParams {
		input := abiFunc.Inputs[i]
		// newInput := tempStructConvert(input)
		if argument.Type, err = abi.NewTypeV2(input.Type, "", input.Components); err != nil {
			return nil, err
		}
		arguments = append(arguments, argument)

		/// arg, err := abi.SolInputTypeConversion(input.Type, v)
		arg, err := argument.Type.StringConvert(v)
		if err != nil {
			return nil, err
		}

		args = append(args, arg)
		/// paramTypes = append(paramTypes, input.Type)
		paramTypes = append(paramTypes, GenFuncSig(input))
	}

	i.typeName = paramTypes
	paramsBytes, err := arguments.PackV2(args...)
	if err != nil {
		/// common.ErrPrintln("pack args error: ", err)
		return nil, err
	}

	// encode the contract method
	funcByte[0] = i.encodeFuncName(funcName)
	funcByte = append(funcByte, paramsBytes)

	/// utl.Logger.Printf("the function byte is %v, the write operation is %v\n", funcByte, isWrite)
	return funcByte, nil
}

func GenFuncSig(input abi.ArgumentMarshaling) string {

	switch input.Type {
	case "tuple":
		var paramTypes []string

		for _, data := range input.Components {
			paramTypes = append(paramTypes, GenFuncSig(data))
		}
		return fmt.Sprintf("(%v)", strings.Join(paramTypes, ","))
	default:
		return input.Type
	}
}

// encodeFuncName encodes the contract method in the way defined by the evm virtual mechine
// Implement the Interpreter interface
func (i *EvmContractInterpreter) encodeFuncName(funcName string) []byte {

	funcNameStr := fmt.Sprintf("%v(%v)", funcName, strings.Join(i.typeName, ","))
	funcNameHash := crypto.Keccak256([]byte(funcNameStr))[:4]
	funcByte := funcNameHash

	return funcByte
}

// combineData packet the data in the way defined by the evm virtual mechine
// Implement the Interpreter interface
func (i EvmContractInterpreter) combineData(funcBytes [][]byte) (string, error) {
	/// utl.Logger.Printf("combine data in evm")
	return hexutil.Encode(bytes.Join(funcBytes, []byte(""))), nil
}

// setIsWrite judge the constant of the contract method based on evm
// Implement the Interpreter interface
func (i EvmContractInterpreter) setIsWrite(abiFunc *FuncDesc) bool {
	return abiFunc.StateMutability != "pure" && abiFunc.StateMutability != "view"
}

func (i EvmContractInterpreter) ReceiptParsing(receipt *Receipt, abiBytes []byte) string {
	var result string

	switch {
	case len(receipt.Logs) != 0:
		result = EventParsingV2(receipt.Logs, abiBytes)
	case receipt.Status == txReceiptFailureCode:
		result = txReceiptFailureMsg
	case receipt.Status == txReceiptSuccessCode:
		result = txReceiptSuccessMsg
	}

	return result
}

func (i EvmContractInterpreter) ParseNonConstantResponse(respStr string, outputType []abi.ArgumentMarshaling) []interface{} {
	var result = make([]interface{}, 1)

	if len(outputType) != 0 {
		/// b, _ := hexutil.Decode(respStr)
		arguments := GenUnpackArgs(outputType)
		result = arguments.ReturnBytesUnpack(respStr)
	} else {
		result[0] = fmt.Sprintf("message call has no return value\n")
	}

	return result
}

//============================Contract WASM===========================

type WasmContractInterpreter struct {
	txType  uint64 // transaction type for contract deployment and execution
	cnsName string // contract name for contract execution by contract name
}

// combineData packet the data in the way defined by the wasm virtual mechine
// Implement the Interpreter interface
func (i WasmContractInterpreter) combineData(funcBytes [][]byte) (string, error) {
	dataParams := make([][]byte, 0)
	dataParams = append(dataParams, common.Int64ToBytes(int64(i.txType)))

	if i.cnsName != "" {
		dataParams = append(dataParams, []byte(i.cnsName))
	}

	// apend function params (contract method and parameters) to data
	dataParams = append(dataParams, funcBytes...)
	/// utl.Logger.Printf("combine data in wasm, dataParam is %v", dataParams)
	return rlpEncode(dataParams)
}

func (i *WasmContractInterpreter) encodeFunction(abiFunc *FuncDesc, funcParams []string, funcName string) ([][]byte, error) {

	var funcByte = make([][]byte, 1)

	// converts the function params to bytes
	for i, v := range funcParams {
		input := abiFunc.Inputs[i]
		p, err := abi.StringConverter(v, input.Type)
		if err != nil {
			return nil, err
		}

		funcByte = append(funcByte, p)
	}

	// encode the contract method
	funcByte[0] = i.encodeFuncName(funcName)

	/// utl.Logger.Printf("the function byte is %v, the write operation is %v\n", funcByte, isWrite)
	return funcByte, nil
}

// encodeFuncName encodes the contract method in the way defined by the wasm virtual mechine
// Implement the Interpreter interface
func (i *WasmContractInterpreter) encodeFuncName(funcName string) []byte {
	/// utl.Logger.Printf("combine functoin in wasm")
	return []byte(funcName)
}

// setIsWrite judge the constant of the contract method based on wasm
// Implement the Interpreter interface
func (i WasmContractInterpreter) setIsWrite(abiFunc *FuncDesc) bool {
	return abiFunc.Constant != "true"
}

func (i WasmContractInterpreter) ReceiptParsing(receipt *Receipt, abiBytes []byte) string {
	// todo: refactor the ReceiptParsing method
	return ReceiptParsing(receipt, abiBytes)
}

func (i WasmContractInterpreter) ParseNonConstantResponse(respStr string, outputType []abi.ArgumentMarshaling) []interface{} {
	var result = make([]interface{}, 1)

	if len(outputType) != 0 {
		b, _ := hexutil.Decode(respStr)
		result[0] = utils.BytesConverter(b, outputType[0].Type)
	} else {
		result[0] = fmt.Sprintf("message call has no return value\n")
	}

	return result
}

//========================DEPLOY EVM=========================

// EvmInterpreter, packet data in the way defined by the evm virtual machine
type EvmDeployInterpreter struct {
	codeBytes []byte // code bytes for evm contract deployment
}

// combineDeployData packet the data in the way defined by the evm virtual mechine
// Implement the Interpreter interface
func (i *EvmDeployInterpreter) combineDeployData() (string, error) {
	return "0x" + string(i.codeBytes), nil
}

func (i EvmDeployInterpreter) ReceiptParsing(receipt *Receipt, abiBytes []byte) string {
	return ReceiptParsing(receipt, abiBytes)
}

//========================DEPLOY WASM=========================

// WasmInterpreter, packet data in the way defined by the evm virtual machine
type WasmDeployInterpreter struct {
	codeBytes []byte // code bytes for wasm contract deployment
	abiBytes  []byte // abi bytes for wasm contract deployment
	txType    uint64 // transaction type for contract deployment and execution
}

// combineDeployData packet the data in the way defined by the wasm virtual mechine
// Implement the Interpreter interface
func (i *WasmDeployInterpreter) combineDeployData() (string, error) {
	/// utl.Logger.Printf("int wasm combineDeployData()")

	dataParams := make([][]byte, 0)
	dataParams = append(dataParams, common.Int64ToBytes(int64(i.txType)))
	dataParams = append(dataParams, i.codeBytes)
	dataParams = append(dataParams, i.abiBytes)

	return rlpEncode(dataParams)
}

func (i WasmDeployInterpreter) ReceiptParsing(receipt *Receipt, abiBytes []byte) string {
	return ReceiptParsing(receipt, abiBytes)
}

//=========================COMMON==============================

// IsWasmContract judge whether the bytes satisfy the code format of wasm virtual machine
func IsWasmContract(codeBytes []byte) bool {
	if bytes.Equal(codeBytes[:8], []byte{0, 97, 115, 109, 1, 0, 0, 0}) {
		return true
	}
	return false
}

// rlpEncode encode the input value by RLP and convert the output bytes to hex string
func rlpEncode(val interface{}) (string, error) {

	dataRlp, err := rlp.EncodeToBytes(val)
	if err != nil {
		return "", fmt.Errorf(utils.ErrRlpEncodeFormat, err.Error())
	}

	return hexutil.Encode(dataRlp), nil

}
