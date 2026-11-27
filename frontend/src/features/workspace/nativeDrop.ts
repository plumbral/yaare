import type {FileLocation} from '../../shared/bridge/files'
export type DropDestination={location:FileLocation;x:number;y:number;at:number}
export function matchNativeDrop(drop:{paths:string[];x:number;y:number},target:DropDestination|null,now:number){
 return target&&now-target.at>=0&&now-target.at<5000&&Math.abs(drop.x-target.x)<=2&&Math.abs(drop.y-target.y)<=2&&drop.paths.length>0&&/^yaare-drop:[a-zA-Z0-9-]{16,64}$/.test(drop.paths[0]!)?{token:drop.paths[0]!,location:target.location}:null
}
