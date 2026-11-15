package epay

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	channelconfig "gorixpay/internal/channel/config"
	"gorixpay/internal/platform/secret"
)

const insertChannelStatement = `INSERT INTO payment_channels
    (id, name, plugin_code, payment_type, priority, weight, status, config_encrypted, created_at, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// channelPluginMap translates the legacy plugin code into the plugin this
// project ships. Anything not listed keeps its legacy code and is imported
// disabled: the console shows it, and enabling it is a deliberate decision
// instead of an accident.
var channelPluginMap = map[string]string{
	"alipay":       "alipay",
	"wxpay":        "wechatpayv3",
	"wechatpay":    "wechatpayv3",
	"wechatpayv3":  "wechatpayv3",
	"qqpay":        "qqpay",
	"unionpay":     "unionpay",
	"epay":         "epay",
	"generic":      "generic",
	"huifu":        "huifu",
	"jeepay":       "jeepay",
	"bepusdt":      "usdt",
	"usdt":         "usdt",
	"sandbox":      "sandbox",
	"waffopancake": "waffopancake",
}

// channelConfigRenames maps legacy config keys onto the names this project's
// plugins read. Only renames that are semantically the same value are listed;
// unknown keys are carried over untouched so nothing is silently dropped.
//
// huifu: the Epay plugin's labels are 「汇付系统号 / 汇付产品号 / 商户私钥 /
// 汇付公钥 / 汇付子商户号」 which is exactly sys_id / product_id / private_key /
// huifu_public_key / huifu_id. Both plugins call /v3/trade/payment/jspay with the
// same envelope, so the credentials move over unchanged.
var channelConfigRenames = map[string]map[string]string{
	"bepusdt": {"appurl": "api_url", "appkey": "key"},
	"huifu": {
		"appid":     "sys_id",
		"appurl":    "product_id",
		"appsecret": "private_key",
		"appkey":    "huifu_public_key",
		"appmchid":  "huifu_id",
	},
}

// migrateChannels maps pre_channel (支付渠道) onto payment_channels.
//
// Every migrated channel lands disabled. The credentials are preserved verbatim
// (encrypted with the same key version the application writes), but whether a
// legacy plugin's key names line up with this project's plugin is something a
// human has to confirm before traffic is routed through it.
func (m *Migrator) migrateChannels(ctx context.Context, source *sourceDB, target *targetStore) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_channel") {
		return skippedTable("payment_channels", "pre_channel", "payment_channels", "源表缺失"), nil
	}
	names, err := legacyPayTypeNames(ctx, source)
	if err != nil {
		return TableReport{}, err
	}
	existing, err := existingChannelNames(ctx, target)
	if err != nil {
		return TableReport{}, err
	}
	// The cipher is only needed when credentials are actually written: a dry run
	// and a key-less run both import the channel rows without a config blob.
	var service *channelconfig.Service
	writeMode := !m.options.DryRun && target != nil && target.writable
	if writeMode && strings.TrimSpace(m.options.MasterKeyFile) != "" {
		built, err := m.channelService()
		if err != nil {
			return TableReport{}, err
		}
		service = built
	}

	statement := source.selectStatement(ctx, "pre_channel",
		[]string{"id", "type", "plugin", "name", "status"},
		[]string{"rate", "paymin", "paymax", "apptype", "daytop", "daymaxorder", "config"},
	)
	rows, err := source.queryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_channel 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows  int64
		unmapped    int64
		missingCfg  int64
		pending     []pendingInsert
		pluginNotes = map[string]int64{}
	)
	for rows.Next() {
		var (
			name, plugin, rate, paymin, paymax sql.NullString
			apptype, daytop, daymaxorder       sql.NullString
			configRaw                          sql.NullString
			id, channelType, status            sql.NullInt64
		)
		if err := rows.Scan(&id, &channelType, &plugin, &name, &status,
			&rate, &paymin, &paymax, &apptype, &daytop, &daymaxorder, &configRaw); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		channelName := strings.TrimSpace(stringValue(name))
		if channelName == "" {
			counts.skip("empty_name")
			continue
		}
		if existing[strings.ToLower(channelName)] {
			counts.skip("existing_name")
			continue
		}
		paymentType, ok := names[intValue(channelType)]
		if !ok {
			counts.fail("pay_type_missing", fmt.Sprintf("channel_id=%d type=%d name=%s", intValue(id), intValue(channelType), channelName))
			continue
		}
		legacyPlugin := strings.ToLower(strings.TrimSpace(stringValue(plugin)))
		mappedPlugin, known := channelPluginMap[legacyPlugin]
		if !known {
			unmapped++
			pluginNotes[legacyPlugin]++
			mappedPlugin = legacyPlugin
		}
		channelID := deterministicID("channel", strings.ToLower(channelName))
		configuration, err := buildChannelConfig(legacyPlugin, stringValue(configRaw))
		if err != nil {
			counts.fail("config_invalid", fmt.Sprintf("channel_id=%d name=%s err=%v", intValue(id), channelName, err))
			continue
		}
		if len(configuration.Values()) == 0 {
			missingCfg++
		}
		encrypted := ""
		if service != nil {
			encrypted, err = service.Encrypt(channelID, configuration)
			if err != nil {
				counts.fail("config_encrypt_failed", fmt.Sprintf("channel_id=%d name=%s err=%v", intValue(id), channelName, err))
				continue
			}
		}
		existing[strings.ToLower(channelName)] = true
		pending = append(pending, pendingInsert{
			key: channelName,
			args: []any{
				channelID,
				trimUTF8(channelName, 100),
				trimUTF8(mappedPlugin, 64),
				trimUTF8(paymentType, 32),
				intValue(id),
				1,
				"disabled",
				encrypted,
				now,
				now,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertChannelStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	report := finishTable("payment_channels", "pre_channel", "payment_channels", sourceRows, counts, 0, 0, false)
	note := "渠道凭据按原键名加密保留，一律以 disabled 导入：启用前请逐条核对插件与参数名"
	if unmapped > 0 {
		note += fmt.Sprintf("；%d 条渠道的插件本项目未内置（%s），保留原插件名待人工处理", unmapped, formatPluginNotes(pluginNotes))
	}
	if missingCfg > 0 {
		note += fmt.Sprintf("；%d 条渠道没有 config，凭据需重新填写", missingCfg)
	}
	if writeMode && service == nil {
		note += "；本次未提供 --master-key-file，渠道凭据没有写入，需在后台重新填写"
	}
	report.Note = note
	return report, nil
}

// channelService returns the encryption service, or nil when no master key was
// supplied: the channel rows are still imported, just without credentials.
func (m *Migrator) channelService() (*channelconfig.Service, error) {
	cipher, err := m.channelCipher()
	if err != nil || cipher == nil {
		return nil, err
	}
	return channelconfig.NewService(cipher), nil
}

// channelCipher builds the same cipher the application uses, lazily: a run that
// only migrates merchants never needs the master key.
func (m *Migrator) channelCipher() (*secret.Cipher, error) {
	if m.cipher != nil {
		return m.cipher, nil
	}
	path := strings.TrimSpace(m.options.MasterKeyFile)
	if path == "" {
		return nil, nil
	}
	key, err := secret.LoadKeyFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 master key 失败: %w", err)
	}
	defer func() {
		for index := range key {
			key[index] = 0
		}
	}()
	cipher, err := secret.NewCipher(key, secret.KeyVersion)
	if err != nil {
		return nil, err
	}
	m.cipher = cipher
	return cipher, nil
}

// existingChannelNames makes the run repeatable: a channel already in the target
// is left alone instead of being duplicated under a new id.
func existingChannelNames(ctx context.Context, target *targetStore) (map[string]bool, error) {
	names := map[string]bool{}
	if target == nil || !target.hasTable("payment_channels") {
		return names, nil
	}
	rows, err := target.db.WithContext(ctx).Raw("SELECT name FROM payment_channels").Rows()
	if err != nil {
		return nil, fmt.Errorf("读取目标 payment_channels 失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[strings.ToLower(name)] = true
	}
	return names, rows.Err()
}

// buildChannelConfig turns the legacy JSON config blob into the configuration
// this project encrypts.
func buildChannelConfig(legacyPlugin, raw string) (channelconfig.Config, error) {
	values := map[string]string{}
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" && trimmed != "null" {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
			return channelconfig.Config{}, fmt.Errorf("config 不是 JSON 对象: %w", err)
		}
		for key, value := range decoded {
			name := strings.TrimSpace(key)
			if name == "" {
				continue
			}
			values[name] = configValueToString(value)
		}
	}
	for from, to := range channelConfigRenames[legacyPlugin] {
		if value, ok := values[from]; ok {
			if _, taken := values[to]; !taken {
				values[to] = value
			}
		}
	}
	configuration := channelconfig.Config{}
	apiURL := strings.TrimSpace(values["api_url"])
	pid := strings.TrimSpace(values["pid"])
	key := strings.TrimSpace(values["key"])
	// The service only accepts the classic triple as a unit (all three or none).
	// A plugin that lives off api_url+key alone therefore keeps every value in
	// Extra, which is what Config.Values() hands to the plugin anyway.
	if apiURL != "" && pid != "" && key != "" {
		configuration.APIURL = apiURL
		configuration.PID = pid
		configuration.Key = key
	}
	extra := map[string]string{}
	for name, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}
		if configuration.APIURL != "" && (name == "api_url" || name == "pid" || name == "key") {
			continue
		}
		extra[name] = trimmedValue
	}
	if len(extra) > 0 {
		configuration.Extra = extra
	}
	return configuration, nil
}

func configValueToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func formatPluginNotes(notes map[string]int64) string {
	keys := make([]string, 0, len(notes))
	for key := range notes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", key, notes[key]))
	}
	return strings.Join(parts, "、")
}
