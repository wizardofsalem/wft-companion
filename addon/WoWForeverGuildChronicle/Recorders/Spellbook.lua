local _, ns = ...

local function num(v)
    v = tonumber(v)
    if v and not ns.IsSecret(v) and v == v and v > -1e9 and v < 1e12 then return v end
end

local function whole(v)
    v = num(v)
    return v and math.floor(v) or nil
end

local function pct(v)
    v = num(v)
    return v and math.floor(v * 10 + 0.5) / 10 or nil
end

local function spellbook()
    if not ns.Enabled("spellbook") or not ns.Func("C_SpellBook.GetNumSpellBookSkillLines") then return end
    local c = ns.Character()
    if not c then return end
    local bank = type(Enum) == "table" and type(Enum.SpellBookSpellBank) == "table" and Enum.SpellBookSpellBank.Player or 0
    local seen, ids = {}, {}
    for line = 1, tonumber((ns.Call("C_SpellBook.GetNumSpellBookSkillLines"))) or 0 do
        local info = ns.Call("C_SpellBook.GetSpellBookSkillLineInfo", line)
        if type(info) == "table" and not ns.IsSecret(info) then
            local offset, count = whole(info.itemIndexOffset) or 0, whole(info.numSpellBookItems) or 0
            for i = offset + 1, offset + count do
                local item = ns.Call("C_SpellBook.GetSpellBookItemInfo", i, bank)
                local id = type(item) == "table" and not ns.IsSecret(item) and whole(item.spellID)
                if id and id > 0 and not seen[id] and #ids < 3000 then seen[id] = true; ids[#ids + 1] = id end
            end
        end
    end
    if #ids == 0 then return end
    table.sort(ids)
    ns.SetState("spellbook", ns.StateKey(c), { spellIds = ids, count = #ids })
end

ns.ReadSpellbook = spellbook
