
package whatif
import ("testing";"net/http/httptest";"strings";"fmt";"budget2/internal/models")
func TestRA1OracleRenderedRevision(t *testing.T) {
 rm,done:=setupTestEnvWithRenderer(t);defer done()
 s,err:=rm.Load();if err!=nil{t.Fatal(err)}
 s.Persons[0].BirthMonth=models.BirthMonthForAge(s.StartDate,60)
 s.ProjectionYears=10;s.PortfolioValue=1200000;s.TaxDeferredPercent=80
 s.TaxConfig=&models.TaxConfig{FilingStatus:models.FilingSingle}
 s.SocialSecurity=&models.SocialSecurityConfig{FRABenefit:2400,FRA:67,ClaimAge:67}
 if err:=rm.Save(s);err!=nil{t.Fatal(err)}
 revision:=rm.Revision()
 w:=httptest.NewRecorder()
 handleRothRecommendations(w,httptest.NewRequest("POST","/whatif/roth-recommendations",nil))
 if w.Code!=200||!strings.Contains(w.Body.String(),fmt.Sprintf("data-roth-revision=%q",fmt.Sprint(revision))) {
  t.Fatalf("actual recommendation output missing generating revision %d: status %d body %s",revision,w.Code,w.Body.String())
 }
}
