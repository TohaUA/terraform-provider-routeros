package routeros

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

type ApiClient struct {
	ctx       context.Context
	HostURL   string
	Username  string
	Password  string
	Transport TransportType
	extra     *ExtraParams
	pool      *apiPool
}

var (
	apiMethodName = map[crudMethod]string{
		crudCreate:      "/add",
		crudRead:        "/print",
		crudUpdate:      "/set",
		crudDelete:      "/remove",
		crudPost:        "/set",
		crudImport:      "/import",
		crudSign:        "/sign",
		crudSignViaScep: "/add-scep",
		crudRemove:      "/remove",
		crudRevoke:      "/issued-revoke",
		crudMove:        "/move",
		crudStart:       "/start",
		crudStop:        "/stop",
		crudGenerateKey: "/generate-key",
		crudExecute:     "",
	}
)

func (c *ApiClient) GetExtraParams() *ExtraParams {
	return c.extra
}

func (c *ApiClient) GetTransport() TransportType {
	return c.Transport
}

func (c *ApiClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {

	// https://help.mikrotik.com/docs/display/ROS/API
	// /interface/vlan/print + '?.id=*39' + '?type=vlan'
	cmd := url.GetApiCmd()
	// The first element is the Path
	cmd[0] += apiMethodName[method]

	// Marshal
	for fieldName, fieldValue := range item {
		cmd = append(cmd, fmt.Sprintf("=%s=%s", fieldName, fieldValue))
	}
	if method == crudExecute {
		ColorizedDebug(c.ctx, "execute command: "+url.Path)
	} else {
		ColorizedDebug(c.ctx, "request body:  "+strings.Join(redactAPIWords(cmd), " "))
	}

	session, err := c.pool.acquire()
	if err != nil {
		return err
	}

	resp, err := session.run(cmd, c.pool.timeout)
	c.pool.release(session, sessionUsable(err))
	if err != nil {
		if method == crudExecute {
			return fmt.Errorf("RouterOS command %s failed", url.Path)
		}
		return err
	}

	if method != crudExecute {
		ColorizedDebug(c.ctx, "response body: "+redactReply(resp))
	}

	if result == nil {
		return nil
	}

	// Unmarshal

	switch r := result.(type) {
	case *MikrotikItem:
		// Only ID returned.
		// !done @ [{`ret` `*7F`}]
		if len(resp.Re) == 0 {
			for k, v := range resp.Done.Map {
				(*r)[k] = v
			}
			break
		}

		// Fill in only one item.
		for k, v := range resp.Re[0].Map {
			(*r)[k] = v
		}
	case *[]MikrotikItem:
		// !re
		for _, sentence := range resp.Re {
			m := MikrotikItem{}
			for k, v := range sentence.Map {
				m[k] = v
			}
			*r = append(*r, m)
		}
		// Some operational commands put their only result in !done, not !re.
		if len(resp.Re) == 0 && resp.Done != nil && len(resp.Done.Map) != 0 {
			row := MikrotikItem{}
			for k, v := range resp.Done.Map {
				row[k] = v
			}
			*r = append(*r, row)
		}

		// !done @ [] is empty...
	default:
		panic("[SendRequest] type " + reflect.TypeOf(result).String() + " is not supported for API response unmarshaling.")
	}

	return nil
}
