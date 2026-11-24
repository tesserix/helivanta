package pharmacy

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
)

// One Tally file's bounds (ADR-0076 §6); a ledger map holds as many heads as its table allows.
var (
	tallyExportVouchers = 5000
	tallyExportBytes    = 8 << 20
	tallyMapEntries     = 1000
)

// tallyMapBodyBytes admits a full map even with every character \u-escaped (about 2.5 MB).
const tallyMapBodyBytes = 3 << 20

var (
	errTallyMapConflict = errors.New("tally ledger map changed or was recorded with another intent")
	errTallyMapFull     = errors.New("tally ledger map exceeds its entries")
	errTallyTooLarge    = errors.New("tally export exceeds its bounds")
	errTallyCurrency    = errors.New("tally export holds a currency other than INR")
	errTallyUnmapped    = errors.New("tally export needs heads the ledger map does not name")
	errTallyMapChanged  = errors.New("tally ledger map is not the one the client previewed")
)

// tallyKinds are the voucher kinds in the order a day's vouchers are written.
var tallyKinds = [...]string{"sale", "credit_note", "supplier_payment", "supplier_payment_reversal"}

func tallyRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/tally/ledger-maps/:map", PermAccountsExport, h.putTallyLedgerMap)
	s.GET("/tally/ledger-maps/:map", PermAccountsExport, h.getTallyLedgerMap)
	s.GET("/tally/preview", PermAccountsExport, h.previewTallyExport)
	s.GET("/tally/vouchers.xml", PermAccountsExport, h.exportTallyVouchers)
}

func tallyMapID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("map"))
	if err != nil || !stocktakeRecordID(id) {
		respond.NotFound(c, "ledger map")
		return uuid.Nil, false
	}
	return id, true
}

func tallyLedgerName(s string) bool {
	return tallyText(s) && strings.TrimSpace(s) == s && utf8.RuneCountInString(s) <= 100
}

func validTallyMap(in TallyLedgerMapInput) bool {
	if in.Replaces != nil && !stocktakeRecordID(*in.Replaces) {
		return false
	}
	seen := make(map[string]bool, len(in.Entries))
	for _, e := range in.Entries {
		if _, ok := tallyHeadLabel(e.Head); !ok || seen[e.Head] || !tallyLedgerName(e.LedgerName) {
			return false
		}
		seen[e.Head] = true
	}
	return true
}

// tallyMapIntent fingerprints a request; heads and names carry no control characters to collide with.
func tallyMapIntent(in TallyLedgerMapInput) string {
	parts := []string{""}
	if in.Replaces != nil {
		parts[0] = in.Replaces.String()
	}
	entries := slices.SortedFunc(slices.Values(in.Entries), func(a, b TallyLedgerMapEntryInput) int { return cmp.Compare(a.Head, b.Head) })
	for _, e := range entries {
		parts = append(parts, e.Head+"\x1f"+e.LedgerName)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1e")))
	return hex.EncodeToString(sum[:])
}

func (h *catalogueHandlers) putTallyLedgerMap(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok || !payableSubject(c, p.Subject, "saving ledger names") {
		return
	}
	id, ok := tallyMapID(c)
	if !ok {
		return
	}
	var in TallyLedgerMapInput
	if !decodeCatalogueWithin(c, &in, tallyMapBodyBytes) {
		return
	}
	if !validTallyMap(in) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "each entry needs a distinct account head Helivanta exports and a Tally ledger name of 1 to 100 characters, without surrounding spaces or control characters")
		return
	}
	digest := tallyMapIntent(in)
	var receipt TallyLedgerMapReceipt
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		// Versions form one chain; serialising them per outlet makes "current" well defined.
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":tally-ledger-map"); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, ActorSubject string }
		if err := tx.Raw(`SELECT intent_sha256,actor_subject FROM pharmacy_tally_ledger_maps WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].IntentSHA256 != digest || existing[0].ActorSubject != p.Subject {
				return errTallyMapConflict
			}
			var err error
			receipt, err = loadTallyMap(tx, p.TenantID, outlet, id)
			return err
		}
		current, names, err := currentTallyMap(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		if (current == nil) != (in.Replaces == nil) || (current != nil && *current != *in.Replaces) {
			return errTallyMapConflict
		}
		for _, e := range in.Entries {
			names[e.Head] = e.LedgerName
		}
		if len(names) > tallyMapEntries {
			return errTallyMapFull
		}
		if err := tx.Exec(`INSERT INTO pharmacy_tally_ledger_maps(id,tenant_id,outlet_id,replaces,entry_count,intent_sha256,actor_subject) VALUES(?,?,?,?,?,?,?)`,
			id, p.TenantID, outlet, in.Replaces, len(names), digest, p.Subject).Error; err != nil {
			return err
		}
		entries, err := json.Marshal(names)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO pharmacy_tally_ledger_map_entries(tenant_id,outlet_id,map_id,head,ledger_name) SELECT ?,?,?,e.key,e.value FROM jsonb_each_text(CAST(? AS jsonb)) e`,
			p.TenantID, outlet, id, string(entries)).Error; err != nil {
			return err
		}
		receipt, err = loadTallyMap(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		tallyError(c, err)
		return
	}
	respond.OK(c, receipt)
}

func (h *catalogueHandlers) getTallyLedgerMap(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := tallyMapID(c)
	if !ok {
		return
	}
	var receipt TallyLedgerMapReceipt
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var err error
		receipt, err = loadTallyMap(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		tallyError(c, err)
		return
	}
	respond.OK(c, receipt)
}

func loadTallyMap(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (TallyLedgerMapReceipt, error) {
	var rows []struct {
		Replaces     *uuid.UUID
		ActorSubject string
		CreatedAt    time.Time
	}
	if err := tx.Raw(`SELECT replaces,actor_subject,created_at FROM pharmacy_tally_ledger_maps WHERE tenant_id=? AND outlet_id=? AND id=?`, tenant, outlet, id).Scan(&rows).Error; err != nil {
		return TallyLedgerMapReceipt{}, err
	}
	if len(rows) != 1 {
		return TallyLedgerMapReceipt{}, gorm.ErrRecordNotFound
	}
	names, err := tallyMapNames(tx, tenant, outlet, id)
	if err != nil {
		return TallyLedgerMapReceipt{}, err
	}
	receipt := TallyLedgerMapReceipt{ID: id, OutletID: outlet, Replaces: rows[0].Replaces, RecordedBy: rows[0].ActorSubject,
		RecordedAt: rows[0].CreatedAt.UTC().Format(time.RFC3339Nano), Entries: make([]TallyLedgerMapEntry, 0, len(names))}
	for _, head := range slices.Sorted(maps.Keys(names)) {
		label, ok := tallyHeadLabel(head)
		if !ok {
			return TallyLedgerMapReceipt{}, errTallyVoucher
		}
		receipt.Entries = append(receipt.Entries, TallyLedgerMapEntry{Label: label, TallyLedgerMapEntryInput: TallyLedgerMapEntryInput{Head: head, LedgerName: names[head]}})
	}
	return receipt, nil
}

func tallyMapNames(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (map[string]string, error) {
	var entries []struct{ Head, LedgerName string }
	if err := tx.Raw(`SELECT head,ledger_name FROM pharmacy_tally_ledger_map_entries WHERE tenant_id=? AND outlet_id=? AND map_id=?`, tenant, outlet, id).Scan(&entries).Error; err != nil {
		return nil, err
	}
	names := make(map[string]string, len(entries))
	for _, e := range entries {
		names[e.Head] = e.LedgerName
	}
	return names, nil
}

// currentTallyMap is the chain's one version without a successor, and its names; nil before the first.
func currentTallyMap(tx *gorm.DB, tenant string, outlet uuid.UUID) (*uuid.UUID, map[string]string, error) {
	var ids []uuid.UUID
	if err := tx.Raw(`SELECT m.id FROM pharmacy_tally_ledger_maps m WHERE m.tenant_id=? AND m.outlet_id=?
		AND NOT EXISTS(SELECT 1 FROM pharmacy_tally_ledger_maps n WHERE n.tenant_id=m.tenant_id AND n.outlet_id=m.outlet_id AND n.replaces=m.id)`, tenant, outlet).Scan(&ids).Error; err != nil {
		return nil, nil, err
	}
	switch len(ids) {
	case 0:
		return nil, map[string]string{}, nil
	case 1:
		names, err := tallyMapNames(tx, tenant, outlet, ids[0])
		return &ids[0], names, err
	}
	return nil, nil, errTallyVoucher
}

// tallyPeriodVouchers reads the period's documents and their entries in one
// statement, so every voucher comes from one snapshot (ADR-0076 §5).
const tallyPeriodVouchers = `WITH docs AS (
 SELECT 0 AS rank,s.id,s.sale_date AS day,s.created_at,s.invoice_number AS number,s.currency,''::text AS related,''::text AS bill,''::text AS supplier,''::text AS supplier_name
 FROM pharmacy_sales s WHERE s.tenant_id=@tenant AND s.outlet_id=@outlet AND s.sale_date BETWEEN CAST(@from AS date) AND CAST(@to AS date)
 UNION ALL
 SELECT 1,n.id,n.credit_note_date,n.created_at,n.credit_note_number,n.currency,s.invoice_number,'','',''
 FROM pharmacy_credit_notes n JOIN pharmacy_sales s ON s.tenant_id=n.tenant_id AND s.outlet_id=n.outlet_id AND s.id=n.sale_id
 WHERE n.tenant_id=@tenant AND n.outlet_id=@outlet AND n.credit_note_date BETWEEN CAST(@from AS date) AND CAST(@to AS date)
 UNION ALL
 SELECT CASE e.kind WHEN 'payment' THEN 2 ELSE 3 END,e.id,e.occurred_on,e.created_at,e.source_reference,e.currency,COALESCE(r.source_reference,''),b.document_reference,b.payee_reference,b.payee_name
 FROM pharmacy_settlements e JOIN pharmacy_payables b ON b.tenant_id=e.tenant_id AND b.outlet_id=e.outlet_id AND b.id=e.payable_id
 LEFT JOIN pharmacy_settlements r ON r.tenant_id=e.tenant_id AND r.outlet_id=e.outlet_id AND r.id=e.reversal_of
 WHERE e.tenant_id=@tenant AND e.outlet_id=@outlet AND e.kind IN ('payment','payment_reversal') AND e.occurred_on BETWEEN CAST(@from AS date) AND CAST(@to AS date)
),
page AS (SELECT * FROM docs ORDER BY day,rank,created_at,id LIMIT @limit),
parts AS (
 SELECT p.id,'line' AS part,l.line_number AS position,'goods' AS treatment,l.supply_nature AS nature,l.rate_ppm,l.taxable_minor,l.cgst_minor,l.sgst_minor,l.igst_minor,''::text AS kind,0::bigint AS amount
 FROM page p JOIN pharmacy_sale_lines l ON l.tenant_id=@tenant AND l.outlet_id=@outlet AND l.sale_id=p.id WHERE p.rank=0
 UNION ALL
 SELECT p.id,'line',1000+c.charge_number,c.tax_treatment,c.supply_nature,c.rate_ppm,c.taxable_minor,c.cgst_minor,c.sgst_minor,c.igst_minor,'',0
 FROM page p JOIN pharmacy_sale_charge_lines c ON c.tenant_id=@tenant AND c.outlet_id=@outlet AND c.sale_id=p.id WHERE p.rank=0
 UNION ALL
 SELECT p.id,'tender',t.tender_number,'','',0,0,0,0,0,t.kind,t.amount_minor
 FROM page p JOIN pharmacy_sale_tenders t ON t.tenant_id=@tenant AND t.outlet_id=@outlet AND t.sale_id=p.id WHERE p.rank=0
 UNION ALL
 SELECT p.id,'line',cl.line_number,'goods',l.supply_nature,l.rate_ppm,cl.taxable_minor,cl.cgst_minor,cl.sgst_minor,cl.igst_minor,'',0
 FROM page p JOIN pharmacy_credit_note_lines cl ON cl.tenant_id=@tenant AND cl.outlet_id=@outlet AND cl.credit_note_id=p.id
 JOIN pharmacy_sale_lines l ON l.tenant_id=cl.tenant_id AND l.outlet_id=cl.outlet_id AND l.sale_id=cl.sale_id AND l.line_number=cl.sale_line WHERE p.rank=1
 UNION ALL
 SELECT p.id,'line',1000+cc.charge_number,c.tax_treatment,c.supply_nature,c.rate_ppm,cc.taxable_minor,cc.cgst_minor,cc.sgst_minor,cc.igst_minor,'',0
 FROM page p JOIN pharmacy_credit_note_charge_lines cc ON cc.tenant_id=@tenant AND cc.outlet_id=@outlet AND cc.credit_note_id=p.id
 JOIN pharmacy_sale_charge_lines c ON c.tenant_id=cc.tenant_id AND c.outlet_id=cc.outlet_id AND c.sale_id=cc.sale_id AND c.charge_number=cc.sale_charge WHERE p.rank=1
 UNION ALL
 SELECT p.id,'tender',f.refund_number,'','',0,0,0,0,0,f.kind,f.amount_minor
 FROM page p JOIN pharmacy_credit_note_refunds f ON f.tenant_id=@tenant AND f.outlet_id=@outlet AND f.credit_note_id=p.id WHERE p.rank=1
 UNION ALL
 SELECT p.id,'settlement',1,'','',0,0,0,0,0,e.method,e.amount_minor
 FROM page p JOIN pharmacy_settlements e ON e.tenant_id=@tenant AND e.outlet_id=@outlet AND e.id=p.id WHERE p.rank>=2
)
SELECT p.rank,p.id,to_char(p.day,'YYYY-MM-DD') AS day,p.number,p.currency,p.related,p.bill,p.supplier,p.supplier_name,
 COALESCE(x.part,'') AS part,COALESCE(x.treatment,'') AS treatment,COALESCE(x.nature,'') AS nature,COALESCE(x.rate_ppm,0) AS rate_ppm,
 COALESCE(x.taxable_minor,0) AS taxable_minor,COALESCE(x.cgst_minor,0) AS cgst_minor,COALESCE(x.sgst_minor,0) AS sgst_minor,COALESCE(x.igst_minor,0) AS igst_minor,COALESCE(x.kind,'') AS kind,COALESCE(x.amount,0) AS amount
FROM page p LEFT JOIN parts x ON x.id=p.id
ORDER BY p.day,p.rank,p.created_at,p.id,x.part,x.position`

type tallyRow struct {
	Rank                    int
	ID                      uuid.UUID
	Day, Number, Currency   string
	Related, Bill           string
	Supplier, SupplierName  string
	Part, Treatment, Nature string
	RatePPM                 int64
	TaxableMinor, CGSTMinor int64
	SGSTMinor               int64
	IGSTMinor               int64
	Kind                    string
	Amount                  int64
}

// tallyPeriod is what a period's file needs: vouchers naming heads, not ledgers.
type tallyPeriod struct {
	vouchers  []tallyVoucher
	counts    [len(tallyKinds)]int64
	heads     []string
	suppliers map[string]string
	mapID     *uuid.UUID
	names     map[string]string
}

func readTallyPeriod(tx *gorm.DB, tenant string, outlet uuid.UUID, from, to string) (tallyPeriod, error) {
	if err := tx.Exec(`SELECT set_config('statement_timeout','8s',true)`).Error; err != nil {
		return tallyPeriod{}, err
	}
	var rows []tallyRow
	if err := tx.Raw(tallyPeriodVouchers, map[string]any{"tenant": tenant, "outlet": outlet, "from": from, "to": to, "limit": tallyExportVouchers + 1}).Scan(&rows).Error; err != nil {
		return tallyPeriod{}, err
	}
	period := tallyPeriod{suppliers: map[string]string{}}
	for i := 0; i < len(rows); {
		end := i + 1
		for end < len(rows) && rows[end].ID == rows[i].ID && rows[end].Rank == rows[i].Rank {
			end++
		}
		if len(period.vouchers) == tallyExportVouchers {
			return tallyPeriod{}, errTallyTooLarge
		}
		doc := rows[i]
		// A Tally company books in rupees, and Helivanta holds no exchange rate.
		if doc.Currency != "INR" {
			return tallyPeriod{}, errTallyCurrency
		}
		v, err := tallyVoucherOf(rows[i:end])
		if err != nil {
			return tallyPeriod{}, err
		}
		if doc.Rank >= 2 {
			period.suppliers["supplier:"+doc.Supplier] = doc.SupplierName
		}
		period.counts[doc.Rank]++
		period.vouchers = append(period.vouchers, v)
		i = end
	}
	heads := map[string]bool{}
	for _, v := range period.vouchers {
		heads[v.Type] = true
		for _, e := range v.Entries {
			heads[e.Ledger] = true
		}
	}
	for head := range heads {
		if _, ok := tallyHeadLabel(head); !ok {
			return tallyPeriod{}, fmt.Errorf("head %q: %w", head, errTallyVoucher)
		}
	}
	if len(heads) > tallyMapEntries {
		return tallyPeriod{}, errTallyTooLarge
	}
	period.heads = slices.Sorted(maps.Keys(heads))
	var err error
	period.mapID, period.names, err = currentTallyMap(tx, tenant, outlet)
	return period, err
}

// tallyVoucherOf posts one document's rows to heads (ADR-0076 §1); the voucher
// balances or the period is refused, so preview and export fail alike.
func tallyVoucherOf(rows []tallyRow) (tallyVoucher, error) {
	doc := rows[0]
	kind := tallyKinds[doc.Rank]
	v := tallyVoucher{Date: doc.Day, Type: "voucher_type:" + kind, Number: doc.Number, RemoteID: "helivanta-" + doc.ID.String()}
	switch kind {
	case "sale":
		v.Narration = "Invoice " + doc.Number
	case "credit_note":
		v.Narration = "Credit note " + doc.Number + " against invoice " + doc.Related
	case "supplier_payment":
		v.Narration = "Payment " + doc.Number + " against bill " + doc.Bill
	default:
		v.Narration = "Reversal " + doc.Number + " of payment " + doc.Related + " against bill " + doc.Bill
	}
	// A credit note reverses its sale; a reversal reverses its payment.
	sales, supplierPaid := kind == "sale", kind == "supplier_payment"
	post := func(head string, debit, party bool, minor int64) {
		if minor != 0 {
			v.Entries = append(v.Entries, tallyEntry{Ledger: head, Debit: debit, Party: party, Minor: minor})
		}
	}
	for _, r := range rows {
		switch r.Part {
		case "line":
			post(tallySalesHead(r.Treatment, r.Nature, r.RatePPM), !sales, false, r.TaxableMinor)
			post(tallyTaxHead("cgst", r.RatePPM), !sales, false, r.CGSTMinor)
			post(tallyTaxHead("sgst", r.RatePPM), !sales, false, r.SGSTMinor)
			post(tallyTaxHead("igst", r.RatePPM), !sales, false, r.IGSTMinor)
		case "tender":
			post("tender:"+r.Kind, sales, false, r.Amount)
		case "settlement":
			post("supplier:"+doc.Supplier, supplierPaid, true, r.Amount)
			post("payment_method:"+r.Kind, !supplierPaid, false, r.Amount)
		}
	}
	if _, err := mergedTallyEntries(v.Entries); err != nil {
		return tallyVoucher{}, fmt.Errorf("voucher %s: %w", v.RemoteID, err)
	}
	return v, nil
}

func tallyMapAudit(period tallyPeriod) string {
	if period.mapID == nil {
		return ""
	}
	return period.mapID.String()
}

func (h *catalogueHandlers) previewTallyExport(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	from, to, ok := outletPeriod(c, "a Tally export")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var period tallyPeriod
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		var err error
		if period, err = readTallyPeriod(tx, p.TenantID, outlet, from, to); err != nil {
			return err
		}
		// The preview names suppliers, so it is a recorded disclosure too.
		return auditRegisterRead(c, tx, "tally_vouchers", p.TenantID, p.Subject, outlet, "accounts_preview", "from", from, "to", to, "ledger_map_id", tallyMapAudit(period))
	})
	if err != nil {
		tallyError(c, err)
		return
	}
	view := TallyExportPreview{OutletID: outlet, From: from, To: to, LedgerMapID: period.mapID, SaleCount: period.counts[0], CreditNoteCount: period.counts[1],
		SupplierPaymentCount: period.counts[2], PaymentReversalCount: period.counts[3], Heads: make([]TallyExportHead, 0, len(period.heads))}
	for _, head := range period.heads {
		label, _ := tallyHeadLabel(head) // readTallyPeriod admitted only labelled heads
		row := TallyExportHead{Head: head, Label: label}
		if name, ok := period.names[head]; ok {
			row.LedgerName = &name
		}
		if name, ok := period.suppliers[head]; ok {
			row.SupplierName = &name
		}
		view.Heads = append(view.Heads, row)
	}
	respond.OK(c, view)
}

// tallyMapFence reads the optional ledger_map the client previewed; an empty value expects no map.
func tallyMapFence(c *gin.Context) (fenced bool, want *uuid.UUID, ok bool) {
	values, present := c.GetQueryArray("ledger_map")
	if !present {
		return false, nil, true
	}
	if len(values) == 1 {
		if values[0] == "" {
			return true, nil, true
		}
		if id, err := uuid.Parse(values[0]); err == nil && stocktakeRecordID(id) {
			return true, &id, true
		}
	}
	respond.Error(c, http.StatusBadRequest, "invalid_request", "ledger_map is the previewed ledger map id, given at most once")
	return false, nil, false
}

func sameTallyMap(want, got *uuid.UUID) bool {
	return want == nil && got == nil || want != nil && got != nil && *want == *got
}

func (h *catalogueHandlers) exportTallyVouchers(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	from, to, ok := outletPeriod(c, "a Tally export")
	if !ok {
		return
	}
	fenced, want, ok := tallyMapFence(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var period tallyPeriod
	var data []byte
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		var err error
		if period, err = readTallyPeriod(tx, p.TenantID, outlet, from, to); err != nil {
			return err
		}
		if fenced && !sameTallyMap(want, period.mapID) {
			return errTallyMapChanged
		}
		if data, err = encodeTallyPeriod(period); err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "tally_vouchers", p.TenantID, p.Subject, outlet, "accounts_export", "from", from, "to", to, "ledger_map_id", tallyMapAudit(period))
	})
	if err != nil {
		tallyError(c, err)
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "pharmacy.tally_export.exported", "Tally vouchers generated", "tenant_id", p.TenantID, "subject", p.Subject,
		"pharmacy_id", currentPharmacy(ctx), "outlet_id", outlet, "from", from, "to", to, "ledger_map_id", tallyMapAudit(period), "sale_count", period.counts[0],
		"credit_note_count", period.counts[1], "supplier_payment_count", period.counts[2], "payment_reversal_count", period.counts[3], "sha256", digest)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-tally-vouchers-%s-%s-%s.xml"`, outlet, from, to))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Voucher-Count", strconv.Itoa(len(period.vouchers)))
	c.Header("X-Export-Ledger-Map", tallyMapAudit(period))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "application/xml; charset=utf-8", data)
}

// encodeTallyPeriod names every head's ledger or writes nothing: never a placeholder.
func encodeTallyPeriod(period tallyPeriod) ([]byte, error) {
	for _, head := range period.heads {
		if _, ok := period.names[head]; !ok {
			return nil, errTallyUnmapped
		}
	}
	vouchers := make([]tallyVoucher, len(period.vouchers))
	for i, v := range period.vouchers {
		v.Type = period.names[v.Type]
		v.Entries = slices.Clone(v.Entries)
		for j := range v.Entries {
			v.Entries[j].Ledger = period.names[v.Entries[j].Ledger]
		}
		vouchers[i] = v
	}
	data, err := encodeTallyVouchers(vouchers)
	if err != nil {
		return nil, err
	}
	if len(data) > tallyExportBytes {
		return nil, errTallyTooLarge
	}
	return data, nil
}

func tallyError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errTallyTooLarge):
		respond.Error(c, http.StatusRequestEntityTooLarge, "accounting_export_too_large",
			fmt.Sprintf("a Tally export holds at most %d vouchers, %d account heads and %d MiB; narrow the period", tallyExportVouchers, tallyMapEntries, tallyExportBytes>>20))
	case errors.Is(err, errTallyCurrency):
		respond.Error(c, http.StatusConflict, "tally_currency_unsupported", "the period holds records in a currency other than INR, which a Tally company keeping rupee books cannot import")
	case errors.Is(err, errTallyUnmapped):
		respond.Error(c, http.StatusConflict, "tally_ledgers_unmapped", "name a Tally ledger for every account head the period needs before exporting")
	case errors.Is(err, errTallyMapChanged):
		respond.Error(c, http.StatusConflict, "tally_ledger_map_changed", "the ledger map changed since the preview; preview the period again before exporting")
	case errors.Is(err, errTallyMapFull):
		respond.Error(c, http.StatusConflict, "tally_ledger_map_full", fmt.Sprintf("a ledger map holds at most %d account heads", tallyMapEntries))
	case errors.Is(err, errTallyMapConflict), errors.As(err, &pg) && pg.Code == "23505":
		respond.Error(c, http.StatusConflict, "tally_ledger_map_conflict", "this ledger map was recorded with other entries, or the current map changed; reload before saving")
	case errors.Is(err, errTallyVoucher):
		respond.InternalErr(c, err, "the Tally export could not be written")
	default:
		catalogueError(c, err)
	}
}
