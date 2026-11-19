local _, ns = ...

local last

local function balance(context, delta, copper)
    if not ns.Enabled("gold") then return end
    copper = copper or ns.Call("GetMoney")
    if not copper then return end
    ns.AddHistory("gold", { at = time(), copper = copper, delta = delta, context = context }, context .. ":" .. ns.NextSeq())
end

ns.On("PLAYER_LOGIN", function()
    last = ns.Call("GetMoney")
    balance("login")
end, "Gold.login")

ns.On("PLAYER_MONEY", function()
    local copper = ns.Call("GetMoney")
    if not copper then return end
    if last and copper ~= last then
        local context = ns.Context()
        ns.Debug("money " .. (copper - last) .. " (" .. context .. ")")
        balance(context, copper - last, copper)
    end
    last = copper
end, "Gold.change")

ns.On("PLAYER_LOGOUT", function()
    if last then balance("logout", nil, last) end
end, "Gold.logout")
