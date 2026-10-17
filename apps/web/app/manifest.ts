import type {MetadataRoute} from "next";
export default function manifest():MetadataRoute.Manifest{return {name:"Learning · 高中学习",short_name:"Learning",description:"广东高中学习与可追溯练习",start_url:"/",display:"standalone",background_color:"#f5f5f7",theme_color:"#5265d9",icons:[{src:"/icon.svg",sizes:"any",type:"image/svg+xml",purpose:"any"}]}}
