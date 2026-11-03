// realtime/room.go：base_id + table_id 纯内存房间。
package realtime

import (
	"sort"

	webapiv1 "excel/api/webapi/v1"
)

type roomKey struct {
	baseID  string
	tableID string
}

type memberState struct {
	principalMember
	connections int
}

type room struct {
	key     roomKey
	clients map[*client]struct{}
	members map[string]*memberState
}

func newRoom(key roomKey) *room {
	return &room{
		key:     key,
		clients: make(map[*client]struct{}),
		members: make(map[string]*memberState),
	}
}

func (room *room) add(client *client) bool {
	room.clients[client] = struct{}{}
	state := room.members[client.member.userID]
	if state == nil {
		room.members[client.member.userID] = &memberState{
			principalMember: client.member,
			connections:     1,
		}
		return true
	}
	state.connections++
	return false
}

func (room *room) remove(client *client) bool {
	delete(room.clients, client)
	state := room.members[client.member.userID]
	if state == nil {
		return false
	}
	state.connections--
	if state.connections > 0 {
		return false
	}
	delete(room.members, client.member.userID)
	return true
}

func (room *room) presenceList() []*webapiv1.MemberPresence {
	userIDs := make([]string, 0, len(room.members))
	for userID := range room.members {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	presence := make([]*webapiv1.MemberPresence, 0, len(userIDs))
	for _, userID := range userIDs {
		member := room.members[userID]
		presence = append(presence, &webapiv1.MemberPresence{
			UserId:   member.userID,
			Color:    member.color,
			Online:   true,
			Internal: member.internal,
		})
	}
	return presence
}
