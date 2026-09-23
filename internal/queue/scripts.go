package queue

import "github.com/redis/go-redis/v9"

var scriptJoin = redis.NewScript(`
local sessionKey = KEYS[1]
local activeKey = KEYS[2]
local seqKey = KEYS[3]
local waitingKey = KEYS[4]

local now = tonumber(ARGV[1])
local maxActive = tonumber(ARGV[2])
local expiry = tonumber(ARGV[3])
local accessToken = ARGV[4]
local dataKeyPfx = ARGV[5]
local sessionID = ARGV[6]
local queueToken = ARGV[7]

local activeCount = redis.call('ZCARD', activeKey)
if activeCount < maxActive then
  redis.call('ZADD', activeKey, expiry, accessToken)
  redis.call('HSET', dataKeyPfx .. accessToken,
    'status', 'allowed',
    'session_id', sessionID,
    'created_at', now,
    'expires_at', expiry)
  redis.call('SET', sessionKey, accessToken)
  return {'allowed', accessToken}
end

local seq = redis.call('INCR', seqKey)
redis.call('ZADD', waitingKey, seq, queueToken)
redis.call('HSET', dataKeyPfx .. queueToken,
  'status', 'waiting',
  'session_id', sessionID,
  'created_at', now,
  'sequence', seq)
redis.call('SET', sessionKey, queueToken)
return {'waiting', queueToken}
`)

var scriptHeartbeat = redis.NewScript(`
local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not score then
  return {'expired', '0'}
end
if tonumber(score) <= tonumber(ARGV[2]) then
  redis.call('ZREM', KEYS[1], ARGV[1])
  redis.call('DEL', KEYS[2] .. ARGV[1])
  return {'expired', '0'}
end

redis.call('ZADD', KEYS[1], ARGV[3], ARGV[1])
redis.call('HSET', KEYS[2] .. ARGV[1], 'expires_at', ARGV[3])
return {'allowed', ARGV[3]}
`)

var scriptLeave = redis.NewScript(`
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[3] .. ARGV[1])
if ARGV[2] ~= '' then
  local current = redis.call('GET', KEYS[4] .. ARGV[2])
  if current == ARGV[1] then
    redis.call('DEL', KEYS[4] .. ARGV[2])
  end
end
return 1
`)

var scriptPromote = redis.NewScript(`
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for _, token in ipairs(expired) do
  local sessionID = redis.call('HGET', KEYS[3] .. token, 'session_id')
  redis.call('ZREM', KEYS[1], token)
  redis.call('DEL', KEYS[3] .. token)
  if sessionID and sessionID ~= '' then
    local current = redis.call('GET', KEYS[4] .. sessionID)
    if current == token then
      redis.call('DEL', KEYS[4] .. sessionID)
    end
  end
end

local activeCount = redis.call('ZCARD', KEYS[1])
local capacity = tonumber(ARGV[2]) - activeCount
if capacity <= 0 then
  return 0
end

local waiting = redis.call('ZRANGE', KEYS[2], 0, capacity - 1)
local promoted = 0
local newExpiry = tonumber(ARGV[1]) + tonumber(ARGV[3])

for _, token in ipairs(waiting) do
  redis.call('ZREM', KEYS[2], token)
  redis.call('ZADD', KEYS[1], newExpiry, token)
  redis.call('HSET', KEYS[3] .. token,
    'status', 'allowed',
    'expires_at', newExpiry)
  promoted = promoted + 1
end

return promoted
`)

var scriptJoinWaiting = redis.NewScript(`
local waitingKey = KEYS[1]
local seqKey = KEYS[2]
local dataKeyPfx = KEYS[3]
local sessionKey = KEYS[4]

local now = tonumber(ARGV[1])
local sessionID = ARGV[2]
local queueToken = ARGV[3]

local seq = redis.call('INCR', seqKey)
redis.call('ZADD', waitingKey, seq, queueToken)
redis.call('HSET', dataKeyPfx .. queueToken,
  'status', 'waiting',
  'session_id', sessionID,
  'created_at', now,
  'sequence', seq)
redis.call('SET', sessionKey, queueToken)
return seq
`)