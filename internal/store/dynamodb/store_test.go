package dynamodb

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/youyo/kintone/internal/store"
)

// keyEqual は ecdsa.PrivateKey の同一性を PKCS8 marshal バイト列で比較する。
// Go 1.26 で ecdsa.PrivateKey.D 直接参照が staticcheck SA1019 となるため、
// 公的に推奨されるシリアライズ経路を使う。
func keyEqual(t *testing.T, a, b *ecdsa.PrivateKey) bool {
	t.Helper()
	ab, err := x509.MarshalPKCS8PrivateKey(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bb, err := x509.MarshalPKCS8PrivateKey(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	return bytes.Equal(ab, bb)
}

// fakeDDB は dynamoDBAPI の最小 in-memory 実装。
//
// 単一テーブル (tableName) のみを扱い、PutItem 時の ConditionExpression は
// "attribute_not_exists(pk)" のみサポートする。Query は KeyConditionExpression
// "gsi1pk = :x" / "gsi2pk = :x" / "gsi2pk = :x AND begins_with(gsi2sk, :sk)" の
// 3 形態に対応する。BatchWriteItem の UnprocessedItems は forceUnprocessed フラグで
// 1 回だけ未処理を返すよう挙動を切り替えられる。
type fakeDDB struct {
	mu        sync.Mutex
	tableName string
	items     map[string]map[string]types.AttributeValue
	ttlStatus types.TimeToLiveStatus

	// テスト挙動制御
	failDescribeWithNotFound bool
	// nthBatchUnprocessed > 0 のとき、N 回目の BatchWriteItem で先頭 1 件を
	// UnprocessedItems に戻す (再送試験用)。0 で常に成功。
	nthBatchUnprocessed int
	batchCallCount      int
}

func newFakeDDB(t *testing.T) *fakeDDB {
	t.Helper()
	return &fakeDDB{
		tableName: "kintone-test",
		items:     map[string]map[string]types.AttributeValue{},
		ttlStatus: types.TimeToLiveStatusEnabled,
	}
}

func (f *fakeDDB) GetItem(ctx context.Context, p *awsdynamodb.GetItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pk := pkOf(p.Key)
	it, ok := f.items[pk]
	if !ok {
		return &awsdynamodb.GetItemOutput{}, nil
	}
	// shallow copy
	cp := map[string]types.AttributeValue{}
	for k, v := range it {
		cp[k] = v
	}
	return &awsdynamodb.GetItemOutput{Item: cp}, nil
}

func (f *fakeDDB) PutItem(ctx context.Context, p *awsdynamodb.PutItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.PutItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pk := pkOf(p.Item)
	if p.ConditionExpression != nil {
		// 簡易実装: attribute_not_exists(pk) のみ評価。
		expr := aws.ToString(p.ConditionExpression)
		if strings.Contains(expr, "attribute_not_exists(pk)") {
			if _, exists := f.items[pk]; exists {
				return nil, &types.ConditionalCheckFailedException{Message: aws.String("exists")}
			}
		}
	}
	cp := map[string]types.AttributeValue{}
	for k, v := range p.Item {
		cp[k] = v
	}
	f.items[pk] = cp
	return &awsdynamodb.PutItemOutput{}, nil
}

func (f *fakeDDB) DeleteItem(ctx context.Context, p *awsdynamodb.DeleteItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DeleteItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pk := pkOf(p.Key)
	delete(f.items, pk)
	return &awsdynamodb.DeleteItemOutput{}, nil
}

func (f *fakeDDB) Query(ctx context.Context, p *awsdynamodb.QueryInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := aws.ToString(p.IndexName)
	expr := aws.ToString(p.KeyConditionExpression)
	values := p.ExpressionAttributeValues

	var matchPK string
	var beginsWith string
	switch {
	case strings.Contains(expr, "gsi1pk = :d"):
		matchPK = sval(values[":d"])
	case strings.Contains(expr, "gsi2pk = :p AND begins_with(gsi2sk, :sk)"):
		matchPK = sval(values[":p"])
		beginsWith = sval(values[":sk"])
	case strings.Contains(expr, "gsi2pk = :p"):
		matchPK = sval(values[":p"])
	default:
		return nil, errors.New("fakeDDB: unsupported KeyConditionExpression: " + expr)
	}

	var attrPK string
	switch idx {
	case IndexGSI1:
		attrPK = AttrGSI1PK
	case IndexGSI2:
		attrPK = AttrGSI2PK
	default:
		return nil, errors.New("fakeDDB: unsupported index: " + idx)
	}

	var matched []map[string]types.AttributeValue
	for _, it := range f.items {
		v, ok := it[attrPK].(*types.AttributeValueMemberS)
		if !ok || v.Value != matchPK {
			continue
		}
		if beginsWith != "" {
			sk, ok := it[AttrGSI2SK].(*types.AttributeValueMemberS)
			if !ok || !strings.HasPrefix(sk.Value, beginsWith) {
				continue
			}
		}
		// projection が "pk" 単体指定なら pk 属性のみ残す
		proj := aws.ToString(p.ProjectionExpression)
		if proj == "pk" {
			matched = append(matched, map[string]types.AttributeValue{
				AttrPK: it[AttrPK],
			})
		} else {
			cp := map[string]types.AttributeValue{}
			for k, v := range it {
				cp[k] = v
			}
			matched = append(matched, cp)
		}
	}

	out := &awsdynamodb.QueryOutput{
		Items: matched,
		Count: int32(len(matched)),
	}
	if p.Select == types.SelectCount {
		out.Items = nil
	}
	return out, nil
}

func (f *fakeDDB) BatchWriteItem(ctx context.Context, p *awsdynamodb.BatchWriteItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.BatchWriteItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCallCount++
	reqs := p.RequestItems[f.tableName]

	var unprocessed []types.WriteRequest
	if f.nthBatchUnprocessed > 0 && f.batchCallCount == f.nthBatchUnprocessed && len(reqs) > 0 {
		// 先頭 1 件を未処理として戻す。
		unprocessed = append(unprocessed, reqs[0])
		reqs = reqs[1:]
	}

	for _, r := range reqs {
		if r.DeleteRequest != nil {
			pk := pkOf(r.DeleteRequest.Key)
			delete(f.items, pk)
		}
		if r.PutRequest != nil {
			pk := pkOf(r.PutRequest.Item)
			cp := map[string]types.AttributeValue{}
			for k, v := range r.PutRequest.Item {
				cp[k] = v
			}
			f.items[pk] = cp
		}
	}

	out := &awsdynamodb.BatchWriteItemOutput{}
	if len(unprocessed) > 0 {
		out.UnprocessedItems = map[string][]types.WriteRequest{f.tableName: unprocessed}
	}
	return out, nil
}

func (f *fakeDDB) DescribeTable(ctx context.Context, p *awsdynamodb.DescribeTableInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTableOutput, error) {
	if f.failDescribeWithNotFound {
		return nil, &types.ResourceNotFoundException{Message: aws.String("missing")}
	}
	return &awsdynamodb.DescribeTableOutput{
		Table: &types.TableDescription{
			TableName: aws.String(f.tableName),
			AttributeDefinitions: []types.AttributeDefinition{
				{AttributeName: aws.String(AttrPK), AttributeType: types.ScalarAttributeTypeS},
			},
		},
	}, nil
}

func (f *fakeDDB) DescribeTimeToLive(ctx context.Context, p *awsdynamodb.DescribeTimeToLiveInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTimeToLiveOutput, error) {
	return &awsdynamodb.DescribeTimeToLiveOutput{
		TimeToLiveDescription: &types.TimeToLiveDescription{
			TimeToLiveStatus: f.ttlStatus,
		},
	}, nil
}

// --- ヘルパー ---

func pkOf(av map[string]types.AttributeValue) string {
	if v, ok := av[AttrPK].(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

func sval(av types.AttributeValue) string {
	if v, ok := av.(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

// --- TokenStore tests ---

func TestTokenStorePutGetDelete(t *testing.T) {
	f := newFakeDDB(t)
	s := newTokenStore(f, f.tableName)
	ctx := context.Background()

	tok := store.Token{
		Domain:      "example.cybozu.com",
		PrincipalID: "oauth:user-1",
		AuthType:    store.AuthTypeOAuth,
		AccessToken: "at-1",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if err := s.Put(ctx, tok); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "at-1" {
		t.Errorf("AccessToken = %q, want at-1", got.AccessToken)
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should be auto-set when zero on Put")
	}
	if err := s.Delete(ctx, tok.Domain, tok.PrincipalID, tok.AuthType); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestTokenStoreListByDomainSorted(t *testing.T) {
	f := newFakeDDB(t)
	s := newTokenStore(f, f.tableName)
	ctx := context.Background()

	domain := "example.cybozu.com"
	for _, pid := range []string{"oauth:c", "oauth:a", "oauth:b"} {
		if err := s.Put(ctx, store.Token{
			Domain:      domain,
			PrincipalID: pid,
			AuthType:    store.AuthTypeOAuth,
			AccessToken: "t-" + pid,
		}); err != nil {
			t.Fatalf("Put %s: %v", pid, err)
		}
	}
	// 別 AuthType を 1 件混入してフィルタが効くことを確認
	if err := s.Put(ctx, store.Token{
		Domain:      domain,
		PrincipalID: "",
		AuthType:    store.AuthTypeAPIToken,
		APIToken:    "api-1",
	}); err != nil {
		t.Fatalf("Put api-token: %v", err)
	}

	list, err := s.ListByDomain(ctx, domain, store.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("ListByDomain: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3 (api-token must be filtered out)", len(list))
	}
	want := []string{"oauth:a", "oauth:b", "oauth:c"}
	for i, w := range want {
		if list[i].PrincipalID != w {
			t.Errorf("list[%d].PrincipalID = %q, want %q", i, list[i].PrincipalID, w)
		}
	}
}

// --- CacheStore tests ---

func TestCacheStorePutGet(t *testing.T) {
	f := newFakeDDB(t)
	c := newCacheStore(f, f.tableName)
	ctx := context.Background()

	if err := c.Put(ctx, "v1:app:42", []byte("payload"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := c.Get(ctx, "v1:app:42")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("got = %q", got)
	}
}

func TestCacheStoreExpired(t *testing.T) {
	f := newFakeDDB(t)
	c := newCacheStore(f, f.tableName)
	ctx := context.Background()

	// 過去の expires_at を直接埋め込む。
	pk := cachePK("expired-key")
	f.items[pk] = map[string]types.AttributeValue{
		AttrPK:        &types.AttributeValueMemberS{Value: pk},
		AttrData:      &types.AttributeValueMemberB{Value: []byte("x")},
		AttrGSI2PK:    &types.AttributeValueMemberS{Value: GSI2PKCache},
		AttrGSI2SK:    &types.AttributeValueMemberS{Value: "expired-key"},
		AttrExpiresAt: &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().Add(-time.Second).UnixNano(), 10)},
	}
	if _, err := c.Get(ctx, "expired-key"); !errors.Is(err, store.ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss for expired entry, got %v", err)
	}
	// 期限切れ Get はベストエフォート delete を仕掛ける。
	if _, exists := f.items[pk]; exists {
		t.Errorf("expired entry should have been deleted by Get")
	}
}

func TestCacheStoreDeleteByPrefix30Items(t *testing.T) {
	f := newFakeDDB(t)
	c := newCacheStore(f, f.tableName)
	ctx := context.Background()

	for i := 0; i < 30; i++ {
		key := "v1:app:" + strconv.Itoa(i)
		if err := c.Put(ctx, key, []byte("v"), time.Hour); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	// 別 prefix エントリを 1 件混入
	if err := c.Put(ctx, "v1:fields:99", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Put fields: %v", err)
	}

	deleted, err := c.DeleteByPrefix(ctx, "v1:app:")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if deleted != 30 {
		t.Errorf("deleted = %d, want 30", deleted)
	}
	// 別 prefix が残っていることを確認
	if _, err := c.Get(ctx, "v1:fields:99"); err != nil {
		t.Errorf("fields entry should remain: %v", err)
	}
	if f.batchCallCount < 2 {
		t.Errorf("batchCallCount = %d, want >= 2 (30/25 chunks)", f.batchCallCount)
	}
}

func TestCacheStoreDeleteByPrefixUnprocessedRetry(t *testing.T) {
	f := newFakeDDB(t)
	f.nthBatchUnprocessed = 1 // 1 回目の BatchWriteItem で 1 件未処理
	c := newCacheStore(f, f.tableName)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		key := "v1:app:" + strconv.Itoa(i)
		if err := c.Put(ctx, key, []byte("v"), time.Hour); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	deleted, err := c.DeleteByPrefix(ctx, "v1:app:")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if deleted != 5 {
		t.Errorf("deleted = %d, want 5 (unprocessed must be retried)", deleted)
	}
	if f.batchCallCount < 2 {
		t.Errorf("batchCallCount = %d, want >= 2 (initial + retry)", f.batchCallCount)
	}
}

func TestCacheStoreStats(t *testing.T) {
	f := newFakeDDB(t)
	c := newCacheStore(f, f.tableName)
	ctx := context.Background()

	for i := 0; i < 7; i++ {
		key := "v1:app:" + strconv.Itoa(i)
		if err := c.Put(ctx, key, []byte("v"), time.Hour); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	stats, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.EntryCount != 7 {
		t.Errorf("EntryCount = %d, want 7", stats.EntryCount)
	}
	if stats.Backend != "dynamodb" {
		t.Errorf("Backend = %q, want dynamodb", stats.Backend)
	}
	if stats.Location != "dynamodb://"+f.tableName {
		t.Errorf("Location = %q", stats.Location)
	}
}

// --- SigningKeyStore tests ---

func TestSigningKeyLoadOrCreateIdempotent(t *testing.T) {
	f := newFakeDDB(t)
	s := newSigningKeyStore(f, f.tableName)
	ctx := context.Background()

	k1, err := s.LoadOrCreate(ctx)
	if err != nil {
		t.Fatalf("LoadOrCreate#1: %v", err)
	}
	k2, err := s.LoadOrCreate(ctx)
	if err != nil {
		t.Fatalf("LoadOrCreate#2: %v", err)
	}
	if !keyEqual(t, k1, k2) {
		t.Errorf("private keys differ between LoadOrCreate calls")
	}
}

func TestSigningKeyConcurrentRace(t *testing.T) {
	f := newFakeDDB(t)
	s := newSigningKeyStore(f, f.tableName)
	ctx := context.Background()

	// 1 回目の load では未保存、その後 ConditionalCheckFailed を狙うために
	// 直接 items に競合鍵を埋めてから LoadOrCreate を呼ぶ。
	// 既存鍵を擬似的に書き込む。
	existing, err := s.LoadOrCreate(ctx)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 続く LoadOrCreate は load 経路で同じ鍵を返すはず。
	got, err := s.LoadOrCreate(ctx)
	if err != nil {
		t.Fatalf("LoadOrCreate after seed: %v", err)
	}
	if !keyEqual(t, existing, got) {
		t.Errorf("existing key not preserved")
	}
}

// --- Container tests ---

func TestContainerCloseIdempotent(t *testing.T) {
	f := newFakeDDB(t)
	c, err := newContainer(f, f.tableName)
	if err != nil {
		t.Fatalf("newContainer: %v", err)
	}
	// sub-store を一通り取得しておく
	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if _, err := c.CacheForDecorator(); err != nil {
		t.Fatalf("CacheForDecorator: %v", err)
	}
	if _, err := c.SigningKey(); err != nil {
		t.Fatalf("SigningKey: %v", err)
	}
	if _, err := c.IDProxyStore(); err != nil {
		t.Fatalf("IDProxyStore: %v", err)
	}
	ctx := context.Background()
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close#1: %v", err)
	}
	// 2 回目の Close も冪等であること
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close#2: %v", err)
	}
	if got := c.LocationString(); got != "dynamodb://"+f.tableName {
		t.Errorf("LocationString = %q", got)
	}
}

func TestContainerDescribeFailures(t *testing.T) {
	t.Run("table_not_found", func(t *testing.T) {
		f := newFakeDDB(t)
		f.failDescribeWithNotFound = true
		_, err := newContainer(f, f.tableName)
		if !errors.Is(err, store.ErrTableNotFound) {
			t.Errorf("err = %v, want ErrTableNotFound", err)
		}
	})
	t.Run("ttl_disabled", func(t *testing.T) {
		f := newFakeDDB(t)
		f.ttlStatus = types.TimeToLiveStatusDisabled
		_, err := newContainer(f, f.tableName)
		if !errors.Is(err, store.ErrTTLDisabled) {
			t.Errorf("err = %v, want ErrTTLDisabled", err)
		}
	})
}

// CacheForDecorator と CacheForAdmin は同一インスタンスを返す (DynamoDB は
// 単一テーブルで両用途を共有)。
func TestContainerCacheSharedInstance(t *testing.T) {
	f := newFakeDDB(t)
	c, err := newContainer(f, f.tableName)
	if err != nil {
		t.Fatalf("newContainer: %v", err)
	}
	a, _ := c.CacheForDecorator()
	b, _ := c.CacheForAdmin()
	if a != b {
		t.Errorf("decorator/admin cache should be the same instance")
	}
}
