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

local function currencies()
    if not ns.Enabled("currencies") or not ns.Func("C_CurrencyInfo.GetCurrencyListSize") then return end
    local c = ns.Character()
    if not c then return end
    local size = tonumber((ns.Call("C_CurrencyInfo.GetCurrencyListSize"))) or 0
    local list = {}
    for i = 1, math.min(size, 200) do
        local info = ns.Call("C_CurrencyInfo.GetCurrencyListInfo", i)
        if type(info) == "table" and not ns.IsSecret(info) and not info.isHeader then
            local id = whole(info.currencyID) or whole(info.currencyTypesID)
            if not id and ns.Func("C_CurrencyInfo.GetCurrencyListLink") then
                local link = ns.Call("C_CurrencyInfo.GetCurrencyListLink", i)
                id = type(link) == "string" and tonumber(link:match("currency:(%d+)")) or nil
            end
            if id and whole(info.quantity) then
                local e = { id = id, quantity = whole(info.quantity) }
                local more = ns.Func("C_CurrencyInfo.GetCurrencyInfo") and ns.Call("C_CurrencyInfo.GetCurrencyInfo", id)
                if type(more) == "table" and not ns.IsSecret(more) then
                    local m = whole(more.maxQuantity)
                    if m and m > 0 then e.max = m end
                    local w = whole(more.quantityEarnedThisWeek)
                    if w and w > 0 then e.weekly = w end
                end
                list[#list + 1] = e
            end
        end
    end
    if #list == 0 then return end
    ns.SetState("currencies", ns.StateKey(c), { currencies = list })
end

ns.ReadCurrencies = currencies
