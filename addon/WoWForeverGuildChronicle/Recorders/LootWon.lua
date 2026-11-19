local _, ns = ...

local MIN_QUALITY = 2
local WON_WINDOW = 120

local FORMATS = {
    { "LOOT_ROLL_WON", "roll" },
    { "LOOT_ITEM_MULTIPLE", "loot" },
    { "LOOT_ITEM", "loot" },
}
local SELF = { "LOOT_ROLL_YOU_WON", "LOOT_ITEM_SELF_MULTIPLE", "LOOT_ITEM_SELF" }
local patterns, selfPatterns
local won = {}

local function buildPatterns()
    patterns = {}
    for _, f in ipairs(FORMATS) do
        local pattern = ns.FormatToPattern(_G[f[1]])
        if pattern then patterns[#patterns + 1] = { pattern = pattern, kind = f[2] } end
    end
    selfPatterns = {}
    for _, name in ipairs(SELF) do
        selfPatterns[#selfPatterns + 1] = ns.FormatToPattern(_G[name])
    end
end

local function parse(line)
    for _, p in ipairs(selfPatterns) do
        if line:match(p) then return end
    end
    for _, p in ipairs(patterns) do
        local caps = { line:match(p.pattern) }
        if #caps > 0 then
            local who, link, count
            for _, c in ipairs(caps) do
                if c:find("item:", 1, true) then
                    link = c
                elseif c:match("^%d+$") then
                    if link and p.kind == "loot" then count = tonumber(c) end
                else
                    who = c
                end
            end
            if count and (count < 1 or count > 10000) then count = nil end
            if who and link then return who, link, count or 1, p.kind end
        end
    end
end

function ns.ParseOthersLoot(line)
    if not patterns then buildPatterns() end
    return parse(line)
end

ns.On("CHAT_MSG_LOOT", function(line)
    if not ns.Enabled("lootWon") or type(line) ~= "string" then return end
    if (tonumber((ns.Call("GetNumGroupMembers"))) or 0) == 0 then return end
    if not patterns then buildPatterns() end
    local who, link, count, kind = parse(line)
    if not who or who == ns.Call("UnitName", "player") then return end
    local itemId = ns.ItemIdFromLink(link)
    local _, _, quality = ns.Call("GetItemInfo", link)
    quality = quality or ns.QualityFromLink(link)
    if not itemId or not quality or quality < MIN_QUALITY then return end
    local key = who .. ":" .. itemId
    local how
    if kind == "roll" then
        won[key] = GetTime()
        how = "roll"
    elseif won[key] and GetTime() - won[key] <= WON_WINDOW then
        won[key] = nil
        return
    else
        how = ns.MasterLoot() and "master" or "loot"
    end
    local member = ns.GroupMember(who)
    local name, _, _, _, _, _, _, instanceId = ns.Call("GetInstanceInfo")
    ns.AddHistory("lootWon", {
        at = time(),
        itemId = itemId,
        name = ns.Clean(ns.ItemNameFromLink(link), 80),
        quality = quality,
        count = count,
        how = how,
        winner = member or { name = ns.Clean(who, 60) },
        instanceId = tonumber(instanceId) and instanceId > 0 and instanceId or nil,
        instance = tonumber(instanceId) and instanceId > 0 and ns.Clean(name, 60) or nil,
    }, key .. ":" .. ns.NextSeq())
    ns.Debug("loot won: " .. who .. " " .. itemId .. " via " .. how)
end, "LootWon.chat")
