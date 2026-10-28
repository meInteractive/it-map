#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <dlfcn.h>
#include <vector>
#include <algorithm>
template<class F> F sym(const char* s){auto p=dlsym(RTLD_DEFAULT,s);if(!p){fprintf(stderr,"Missing ICU symbol %s\n",s);abort();}return reinterpret_cast<F>(p);}
extern "C" void* napi_register_module_v1(void*,void* exports){
 auto open=sym<void*(*)(const char*,int32_t*)>("ucol_open_78");
 auto close=sym<void(*)(void*)>("ucol_close_78");
 auto key=sym<int32_t(*)(const void*,const char16_t*,int32_t,uint8_t*,int32_t)>("ucol_getSortKey_78");
 auto attr=sym<void(*)(void*,int,int,int32_t*)>("ucol_setAttribute_78");
 auto usnew=sym<void(*)(void*,const char16_t*,int32_t)>("_ZN6icu_7813UnicodeStringC1EPKDsi");
 auto usdelete=sym<void(*)(void*)>("_ZN6icu_7813UnicodeStringD1Ev");
 auto uvnew=sym<void(*)(void*,int32_t*)>("_ZN6icu_789UVector64C1ER10UErrorCode");
 auto uvdelete=sym<void(*)(void*)>("_ZN6icu_789UVector64D1Ev");
 auto ces=sym<void(*)(const void*,const void*,void*,int32_t*)>("_ZNK6icu_7817RuleBasedCollator14internalGetCEsERKNS_13UnicodeStringERNS_9UVector64ER10UErrorCode");
 int32_t err=0;void*c=open("ru",&err);if(err>0)abort();attr(c,4,17,&err);

 auto nfdget=sym<const void*(*)(int32_t*)>("unorm2_getNFDInstance_78");auto normalize=sym<int32_t(*)(const void*,const char16_t*,int32_t,char16_t*,int32_t,int32_t*)>("unorm2_normalize_78");int32_t normerr=0;const void*nfd=nfdget(&normerr);if(normerr>0)abort();
 auto combining=sym<uint8_t(*)(int32_t)>("u_getCombiningClass_78");
 auto setopen=sym<void*(*)()>("uset_openEmpty_78");auto setclose=sym<void(*)(void*)>("uset_close_78");
 auto contractions=sym<void(*)(const void*,void*,void*,int8_t,int32_t*)>("ucol_getContractionsAndExpansions_78");
 auto setcount=sym<int32_t(*)(const void*)>("uset_getItemCount_78");
 auto setitem=sym<int32_t(*)(const void*,int32_t,int32_t*,int32_t*,char16_t*,int32_t,int32_t*)>("uset_getItem_78");
 attr(c,5,0,&err); // primary strength only for recovering the script reorder map
 auto output=[&](int cp,const char16_t*text,int len){
  alignas(16) char us[256]={},uv[256]={};usnew(us,text,len);err=0;uvnew(uv,&err);ces(c,us,uv,&err);
  int32_t count=*reinterpret_cast<int32_t*>(uv+8),capacity=*reinterpret_cast<int32_t*>(uv+12);int64_t*data=*reinterpret_cast<int64_t**>(uv+24);
  if(err>0||count<0||count>256||capacity<count||!data){fprintf(stderr,"Invalid pinned ICU vector layout\n");abort();}
  printf("[%d,[",cp);for(int i=0;i<count;i++){uint64_t ce=data[i];if(i)printf(",");printf("[%u,%u,%u]",(uint32_t)(ce>>32),(uint32_t)((ce>>16)&65535),(uint32_t)(ce&0x3f3f));}printf("]");
  if(cp>=0){printf(",%u",combining(cp));uint8_t bytes[512];int n=key(c,text,len,bytes,512);if(n<=0||n>512)abort();printf(",[");for(int i=0;i<n;i++){if(i)printf(",");printf("%u",bytes[i]);}printf("]");}
  else{printf(",[");for(int i=0;i<len;i++){if(i)printf(",");printf("%u",(uint32_t)text[i]);}printf("]");
   char16_t normalized[8192];err=0;int nl=normalize(nfd,text,len,normalized,8192,&err);if(err>0||nl>8192)abort();
   alignas(16) char nus[256]={},nuv[256]={};usnew(nus,normalized,nl);uvnew(nuv,&err);ces(c,nus,nuv,&err);int nc=*reinterpret_cast<int32_t*>(nuv+8);int64_t*nd=*reinterpret_cast<int64_t**>(nuv+24);int ncap=*reinterpret_cast<int32_t*>(nuv+12);if(err>0||nc<0||nc>256||ncap<nc||!nd)abort();
   printf(",[");for(int i=0;i<nc;i++){uint64_t ce=nd[i];if(i)printf(",");printf("[%u,%u,%u]",(uint32_t)(ce>>32),(uint32_t)((ce>>16)&65535),(uint32_t)(ce&0x3f3f));}printf("]");uvdelete(nuv);usdelete(nus);
  }
  printf("]\n");uvdelete(uv);usdelete(us);
 };
 for(uint32_t cp=0;cp<=0x10ffff;cp++){char16_t text[2];int32_t len=1;if(cp>0xffff){text[0]=0xd800+((cp-0x10000)>>10);text[1]=0xdc00+((cp-0x10000)&1023);len=2;}else text[0]=cp;output(cp,text,len);}
 void*set=setopen();err=0;contractions(c,set,nullptr,1,&err);if(err>0)abort();
 for(int i=0;i<setcount(set);i++){char16_t text[4096];int32_t start,end;err=0;int len=setitem(set,i,&start,&end,text,4096,&err);if(err>0||len>4096)abort();if(len>0)output(-1,text,len);}
 setclose(set);
 close(c);return exports;
}
