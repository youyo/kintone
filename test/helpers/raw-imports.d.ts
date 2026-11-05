// vite の `?raw` import(README などの文書を文字列で読む)の型。
declare module "*.md?raw" {
  const content: string;
  export default content;
}
