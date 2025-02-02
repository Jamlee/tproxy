package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"github.com/kevwan/tproxy/display"
	"github.com/mongodb/mongo-go-driver/bson"
	"io"
)

const (
	OP_REPLY        = 1    //Reply to a client request. responseTo is set.
	OP_UPDATE       = 2001 //Update document.
	OP_INSERT       = 2002 //Insert new document.
	RESERVED        = 2003 //Formerly used for OP_GET_BY_OID.
	OP_QUERY        = 2004 //Query a collection.
	OP_GET_MORE     = 2005 //Get more data from a query. See Cursors.
	OP_DELETE       = 2006 //Delete documents.
	OP_KILL_CURSORS = 2007 //Notify database that the client has finished with the cursor.
	OP_COMMAND      = 2010 //Cluster internal protocol representing a command request.
	OP_COMMANDREPLY = 2011 //Cluster internal protocol representing a reply to an OP_COMMAND.
	OP_MSG          = 2013 //Send a message using the format introduced in MongoDB 3.6.
)

type mongoInterop struct {
}

type packet struct {
	IsClientFlow  bool //client->server
	MessageLength int
	OpCode        int //request type
	Payload       io.Reader
}

func (mongo *mongoInterop) Dump(r io.Reader, source string, id int, quiet bool) {
	var pk *packet
	for {
		pk = newPacket(source, r)
		if pk == nil {
			return
		}
		if pk.IsClientFlow {
			resolveClientPacket(pk)
		}
	}
}

func resolveClientPacket(pk *packet) {
	var msg string
	switch pk.OpCode {
	case OP_UPDATE:
		zero := readInt32(pk.Payload)
		fullCollectionName := readString(pk.Payload)
		flags := readInt32(pk.Payload)
		selector := readBson2Json(pk.Payload)
		update := readBson2Json(pk.Payload)
		_ = zero
		_ = flags

		msg = fmt.Sprintf(" [Update] [coll:%s] %v %v",
			fullCollectionName,
			selector,
			update,
		)

	case OP_INSERT:
		flags := readInt32(pk.Payload)
		fullCollectionName := readString(pk.Payload)
		command := readBson2Json(pk.Payload)
		_ = flags

		msg = fmt.Sprintf(" [Insert] [coll:%s] %v",
			fullCollectionName,
			command,
		)

	case OP_QUERY:
		flags := readInt32(pk.Payload)
		fullCollectionName := readString(pk.Payload)
		numberToSkip := readInt32(pk.Payload)
		numberToReturn := readInt32(pk.Payload)
		_ = flags
		_ = numberToSkip
		_ = numberToReturn

		command := readBson2Json(pk.Payload)
		selector := readBson2Json(pk.Payload)

		msg = fmt.Sprintf(" [Query] [coll:%s] %v %v",
			fullCollectionName,
			command,
			selector,
		)

	case OP_COMMAND:
		database := readString(pk.Payload)
		commandName := readString(pk.Payload)
		metaData := readBson2Json(pk.Payload)
		commandArgs := readBson2Json(pk.Payload)
		inputDocs := readBson2Json(pk.Payload)

		msg = fmt.Sprintf(" [Commend] [DB:%s] [Cmd:%s] %v %v %v",
			database,
			commandName,
			metaData,
			commandArgs,
			inputDocs,
		)

	case OP_GET_MORE:
		zero := readInt32(pk.Payload)
		fullCollectionName := readString(pk.Payload)
		numberToReturn := readInt32(pk.Payload)
		cursorId := readInt64(pk.Payload)
		_ = zero

		msg = fmt.Sprintf(" [Query more] [coll:%s] [num of reply:%v] [cursor:%v]",
			fullCollectionName,
			numberToReturn,
			cursorId,
		)

	case OP_DELETE:
		zero := readInt32(pk.Payload)
		fullCollectionName := readString(pk.Payload)
		flags := readInt32(pk.Payload)
		selector := readBson2Json(pk.Payload)
		_ = zero
		_ = flags

		msg = fmt.Sprintf(" [Delete] [coll:%s] %v",
			fullCollectionName,
			selector,
		)

	case OP_MSG:
		return
	default:
		return
	}

	display.PrintlnWithTime(getDirectionStr(true) + msg)
}

func newPacket(source string, r io.Reader) *packet {
	//read pk
	var pk *packet
	var err error
	pk, err = parsePacket(r)

	//stream close
	if err == io.EOF {
		display.PrintlnWithTime(" close")
		return nil
	} else if err != nil {
		display.PrintlnWithTime("ERR : Unknown stream", ":", err)
		return nil
	}

	// set flow direction
	if source == "SERVER" {
		pk.IsClientFlow = false
	} else {
		pk.IsClientFlow = true
	}

	return pk
}

func parsePacket(r io.Reader) (*packet, error) {
	var buf bytes.Buffer
	p := &packet{}

	//header
	header := make([]byte, 16)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	// message length
	payloadLen := binary.LittleEndian.Uint32(header[0:4]) - 16
	p.MessageLength = int(payloadLen)

	// OpCode
	p.OpCode = int(binary.LittleEndian.Uint32(header[12:]))

	if p.MessageLength != 0 {
		io.CopyN(&buf, r, int64(payloadLen))
	}

	p.Payload = bytes.NewReader(buf.Bytes())

	return p, nil
}

func getDirectionStr(isClient bool) string {
	var msg string
	if isClient {
		msg += "| cli -> ser |"
	} else {
		msg += "| ser -> cli |"
	}
	return color.HiBlueString("%s", msg)
}

func readInt32(r io.Reader) (n int32) {
	binary.Read(r, binary.LittleEndian, &n)
	return
}

func readInt64(r io.Reader) int64 {
	var n int64
	binary.Read(r, binary.LittleEndian, &n)
	return n
}

func readString(r io.Reader) string {
	var result []byte
	var b = make([]byte, 1)
	for {

		_, err := r.Read(b)

		if err != nil {
			panic(err)
		}

		if b[0] == '\x00' {
			break
		}

		result = append(result, b[0])
	}

	return string(result)
}

func readBson2Json(r io.Reader) string {
	//read len
	docLen := readInt32(r)
	if docLen == 0 {
		return ""
	}

	//document []byte
	docBytes := make([]byte, int(docLen))
	binary.LittleEndian.PutUint32(docBytes, uint32(docLen))
	if _, err := io.ReadFull(r, docBytes[4:]); err != nil {
		panic(err)
	}

	//resolve document
	var bsn bson.M
	err := bson.Unmarshal(docBytes, &bsn)
	if err != nil {
		panic(err)
	}

	//format to Json
	jsonStr, err := json.Marshal(bsn)
	if err != nil {
		return fmt.Sprintf("{\"error\":%s}", err.Error())
	}
	return string(jsonStr)
}
